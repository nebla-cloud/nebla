-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "instance_generation" bigint NOT NULL DEFAULT 0;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "instance_generation";
