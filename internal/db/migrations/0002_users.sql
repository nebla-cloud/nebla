-- +goose Up
create table users (
    id bigserial primary key,
    email text not null unique,
    name text not null,
    password_hash text not null,
    is_admin boolean not null default false,
    totp_secret text,
    totp_pending_secret text,
    totp_enabled boolean not null default false,
    created_at timestamptz not null default now()
);

create table sessions (
    token_hash bytea primary key,
    user_id bigint not null references users(id) on delete cascade,
    expires_at timestamptz not null,
    created_at timestamptz not null default now()
);
create index sessions_user_id_idx on sessions (user_id);

create table settings (
    key text primary key,
    value text not null
);

-- +goose Down
drop table settings;
drop table sessions;
drop table users;
