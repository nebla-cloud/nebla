-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "clone_nonce" text NULL;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "clone_nonce";
