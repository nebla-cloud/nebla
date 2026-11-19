-- +goose Up
create table schema_info (
    id integer primary key default 1 check (id = 1),
    created_at timestamptz not null default now()
);
insert into schema_info (id) values (1);

-- +goose Down
drop table schema_info;
