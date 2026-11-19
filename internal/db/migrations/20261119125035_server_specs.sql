-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "cores" bigint NOT NULL, ADD COLUMN "memory_mb" bigint NOT NULL;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "memory_mb", DROP COLUMN "cores";
