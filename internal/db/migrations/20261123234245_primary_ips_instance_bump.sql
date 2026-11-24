-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "instance_bump_pending" boolean NOT NULL DEFAULT false;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "instance_bump_pending";
