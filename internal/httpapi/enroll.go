package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/enroll"
	"github.com/nebla-cloud/nebla/internal/realip"
)

var (
	enrollTokenRe    = regexp.MustCompile(`^nbe_[A-Za-z0-9_-]{43}$`)
	errEnrollInvalid = apierr.New(http.StatusUnauthorized, "enrollment_invalid", "enrollment token is not valid")
)

// enrollAllow applies the per-IP enroll limiter.
func (s *server) enrollAllow(ctx context.Context) error {
	r := requestFrom(ctx)
	if r == nil {
		return nil
	}
	res := s.enrollLimit.Reserve("ip:" + realip.ClientIP(r, s.deps.Config.TrustedProxies))
	if res.Allowed {
		return nil
	}
	if st := rlFrom(ctx); st != nil {
		st.w.Header().Set("Retry-After", strconv.Itoa(max(ceilSeconds(res.RetryAfter), 1)))
	}
	return apierr.New(http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests")
}

// enrollToken reads the Authorization: Enroll header.
func enrollToken(ctx context.Context) (string, error) {
	r := requestFrom(ctx)
	if r == nil {
		return "", errEnrollInvalid
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Enroll ")
	if !ok || !enrollTokenRe.MatchString(tok) {
		return "", errEnrollInvalid
	}
	return tok, nil
}

func apiEnrollment(e enroll.Enrollment) apigen.LocationEnrollment {
	out := apigen.LocationEnrollment{Id: e.ID, Name: e.Name, Description: e.Description, LocationId: e.LocationID,
		TokenName: e.TokenName, State: apigen.LocationEnrollmentState(e.State), Expired: e.Expired, ExpiresAt: e.ExpiresAt,
		Created: e.CreatedAt}
	if e.Report != nil {
		rep := apigen.EnrollmentReport{PveVersion: e.Report.PVEVersion, Node: e.Report.Node, TokenId: e.Report.TokenID,
			Nodes:    make([]apigen.EnrollmentReportNode, len(e.Report.Nodes)),
			Storages: make([]apigen.EnrollmentReportStorage, len(e.Report.Storages))}
		for i, n := range e.Report.Nodes {
			rep.Nodes[i] = apigen.EnrollmentReportNode{Name: n.Name, Ip: n.IP, Online: n.Online, Fingerprint: n.Fingerprint}
		}
		for i, st := range e.Report.Storages {
			rep.Storages[i] = apigen.EnrollmentReportStorage{Id: st.ID, Type: st.Type, Content: st.Content, Shared: st.Shared,
				NeblaIso: st.NeblaISO}
			if rep.Storages[i].Content == nil {
				rep.Storages[i].Content = []string{}
			}
		}
		out.Report = &rep
	}
	return out
}

func apiEnrollmentCreated(c enroll.Created) apigen.LocationEnrollmentCreated {
	return apigen.LocationEnrollmentCreated{Enrollment: apiEnrollment(c.Enrollment), Token: c.Token, Command: c.Command,
		OfflineScript: c.OfflineScript}
}

func (s *server) GetEnrollScript(ctx context.Context, _ apigen.GetEnrollScriptRequestObject) (apigen.GetEnrollScriptResponseObject, error) {
	if err := s.enrollAllow(ctx); err != nil {
		return nil, err
	}
	b := s.enroll.ScriptBody()
	return apigen.GetEnrollScript200TextplainCharsetUtf8Response{Body: bytes.NewReader(b), ContentLength: int64(len(b))}, nil
}

func (s *server) GetEnrollCheck(ctx context.Context, _ apigen.GetEnrollCheckRequestObject) (apigen.GetEnrollCheckResponseObject, error) {
	if err := s.enrollAllow(ctx); err != nil {
		return nil, err
	}
	tok, err := enrollToken(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.enroll.Check(ctx, tok)
	if err != nil {
		return nil, err
	}
	return apigen.GetEnrollCheck200JSONResponse{TokenName: e.TokenName, Location: e.Name, ExpiresAt: e.ExpiresAt}, nil
}

func (s *server) PostEnrollReport(ctx context.Context, req apigen.PostEnrollReportRequestObject) (apigen.PostEnrollReportResponseObject, error) {
	if err := s.enrollAllow(ctx); err != nil {
		return nil, err
	}
	tok, err := enrollToken(ctx)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if req.Body != nil {
		raw = *req.Body
	}
	if err := s.enroll.Report(ctx, tok, raw); err != nil {
		return nil, err
	}
	return apigen.PostEnrollReport204Response{}, nil
}

func (s *server) GetAdminLocationEnrollments(ctx context.Context, _ apigen.GetAdminLocationEnrollmentsRequestObject) (apigen.GetAdminLocationEnrollmentsResponseObject, error) {
	es, err := s.enroll.List(ctx)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminLocationEnrollments200JSONResponse{Enrollments: make([]apigen.LocationEnrollment, len(es))}
	for i, e := range es {
		out.Enrollments[i] = apiEnrollment(e)
	}
	return out, nil
}

func (s *server) PostAdminLocationEnrollments(ctx context.Context, req apigen.PostAdminLocationEnrollmentsRequestObject) (apigen.PostAdminLocationEnrollmentsResponseObject, error) {
	b := req.Body
	if (b.Name != nil) == (b.LocationId != nil) {
		return nil, apierr.New(http.StatusUnprocessableEntity, "invalid_input", "give exactly one of name or location_id")
	}
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, errUnauthorized
	}
	c, err := s.enroll.Create(ctx, enroll.CreateInput{Name: deref(b.Name), Description: deref(b.Description), LocationID: b.LocationId}, u.ID)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminLocationEnrollments201JSONResponse(apiEnrollmentCreated(c)), nil
}

func (s *server) GetAdminLocationEnrollment(ctx context.Context, req apigen.GetAdminLocationEnrollmentRequestObject) (apigen.GetAdminLocationEnrollmentResponseObject, error) {
	e, err := s.enroll.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetAdminLocationEnrollment200JSONResponse(apiEnrollment(e)), nil
}

func (s *server) DeleteAdminLocationEnrollment(ctx context.Context, req apigen.DeleteAdminLocationEnrollmentRequestObject) (apigen.DeleteAdminLocationEnrollmentResponseObject, error) {
	if err := s.enroll.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminLocationEnrollment204Response{}, nil
}

func (s *server) PostAdminLocationEnrollmentToken(ctx context.Context, req apigen.PostAdminLocationEnrollmentTokenRequestObject) (apigen.PostAdminLocationEnrollmentTokenResponseObject, error) {
	c, err := s.enroll.NewToken(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminLocationEnrollmentToken200JSONResponse(apiEnrollmentCreated(c)), nil
}

func (s *server) PostAdminLocationEnrollmentReport(ctx context.Context, req apigen.PostAdminLocationEnrollmentReportRequestObject) (apigen.PostAdminLocationEnrollmentReportResponseObject, error) {
	if err := s.enroll.ReportBlob(ctx, req.Id, req.Body.Blob); err != nil {
		return nil, err
	}
	e, err := s.enroll.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminLocationEnrollmentReport200JSONResponse(apiEnrollment(e)), nil
}

func (s *server) PostAdminLocationEnrollmentFinish(ctx context.Context, req apigen.PostAdminLocationEnrollmentFinishRequestObject) (apigen.PostAdminLocationEnrollmentFinishResponseObject, error) {
	b := req.Body
	f, err := s.enroll.Finish(ctx, req.Id, enroll.FinishInput{URL: b.Url, DiskStorage: b.DiskStorage, ISOStorage: b.IsoStorage,
		ImportStorage: deref(b.ImportStorage)})
	if err != nil {
		return nil, err
	}
	out := apigen.PostAdminLocationEnrollmentFinish200JSONResponse{Location: apiAdminLocation(f.Location), Cleanup: f.Cleanup}
	if out.Cleanup == nil {
		out.Cleanup = []string{}
	}
	return out, nil
}
