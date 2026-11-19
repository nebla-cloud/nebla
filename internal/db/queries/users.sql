-- name: CountUsers :one
select count(*) from users;

-- name: CreateUser :one
insert into users (email, name, password_hash, is_admin)
values ($1, $2, $3, $4)
returning *;

-- name: GetUserByEmail :one
select * from users where email = $1;

-- name: GetUser :one
select * from users where id = $1;

-- name: SetTOTPPending :exec
update users set totp_pending_secret = $2 where id = $1;

-- name: EnableTOTP :exec
update users
set totp_secret = totp_pending_secret, totp_pending_secret = null, totp_enabled = true
where id = $1;

-- name: DisableTOTP :exec
update users
set totp_secret = null, totp_pending_secret = null, totp_enabled = false
where id = $1;

-- name: CreateSession :exec
insert into sessions (token_hash, user_id, expires_at) values ($1, $2, $3);

-- name: GetSessionUser :one
select u.* from sessions s
join users u on u.id = s.user_id
where s.token_hash = $1 and s.expires_at > now();

-- name: DeleteSession :exec
delete from sessions where token_hash = $1;

-- name: DeleteExpiredSessions :exec
delete from sessions where expires_at <= now();

-- name: GetSetting :one
select value from settings where key = $1;

-- name: UpsertSetting :exec
insert into settings (key, value) values ($1, $2)
on conflict (key) do update set value = excluded.value;
