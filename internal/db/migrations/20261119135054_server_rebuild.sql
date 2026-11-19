-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "rebuild_vmid" bigint NULL, ADD COLUMN "rebuild_clone_upid" text NULL, ADD COLUMN "rebuild_clone_nonce" text NULL, ADD COLUMN "rebuild_step" bigint NOT NULL DEFAULT 0;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "rebuild_step", DROP COLUMN "rebuild_clone_nonce", DROP COLUMN "rebuild_clone_upid", DROP COLUMN "rebuild_vmid";
