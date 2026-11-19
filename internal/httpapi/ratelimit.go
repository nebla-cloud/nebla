package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/ratelimit"
	"github.com/Hexeption/nebla/internal/realip"
	"golang.org/x/time/rate"
)

const (
	defaultAPIBurst = 100
	rateKey         = ctxKey(100)
)

// defaultAPIRate is 3600 requests per hour.
var defaultAPIRate = rate.Every(time.Hour / 3600)

// rlState is the per-request limit state; the strict auth step fills it in once the principal is known.
type rlState struct {
	s    *server
	w    http.ResponseWriter
	ip   string
	done bool
}

// consume takes one token for key, writes the RateLimit headers, and returns 429 when empty.
func (st *rlState) consume(key string) error {
	if st == nil || st.done {
		return nil
	}
	st.done = true
	res := st.s.apiLimit.Reserve(key)
	h := st.w.Header()
	h.Set("RateLimit-Limit", strconv.Itoa(int(float64(st.s.apiLimit.Rate())*3600+0.5)))
	h.Set("RateLimit-Remaining", strconv.Itoa(res.Remaining))
	h.Set("RateLimit-Reset", strconv.Itoa(ceilSeconds(res.Reset)))
	if res.Allowed {
		return nil
	}
	h.Set("Retry-After", strconv.Itoa(max(ceilSeconds(res.RetryAfter), 1)))
	return apierr.New(http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests")
}

func ceilSeconds(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }

func rlFrom(ctx context.Context) *rlState {
	st, _ := ctx.Value(rateKey).(*rlState)
	return st
}

func (st *rlState) byIP() error {
	if st == nil {
		return nil
	}
	return st.consume("ip:" + st.ip)
}

func newAPILimiter(l *ratelimit.Limiter) *ratelimit.Limiter {
	if l == nil {
		return ratelimit.New(defaultAPIRate, defaultAPIBurst)
	}
	return l
}

// rateLimitMiddleware limits anonymous requests by IP; authenticate limits credentialed ones by principal.
func (s *server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &rlState{s: s, w: w, ip: realip.ClientIP(r, s.deps.Config.TrustedProxies)}
		rw := &rlWriter{ResponseWriter: w, st: st}
		if r.Header.Get("Authorization") == "" {
			if _, cerr := r.Cookie(sessionCookie); cerr != nil {
				if err := st.byIP(); err != nil {
					s.responseError(rw, r, err)
					return
				}
			}
		}
		next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), rateKey, st)))
	})
}

// rlWriter applies the IP limit on first write if nothing consumed a token yet.
type rlWriter struct {
	http.ResponseWriter
	st *rlState
}

func (w *rlWriter) WriteHeader(code int) {
	_ = w.st.byIP()
	w.ResponseWriter.WriteHeader(code)
}

func (w *rlWriter) Write(b []byte) (int, error) {
	_ = w.st.byIP()
	return w.ResponseWriter.Write(b)
}

func (w *rlWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
