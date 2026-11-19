-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "iso_generation" bigint NOT NULL DEFAULT 1, ADD COLUMN "create_step" bigint NOT NULL DEFAULT 0;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "create_step", DROP COLUMN "iso_generation";
