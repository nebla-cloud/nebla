-- +goose Up
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "iso_lease_until" timestamptz NULL;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "iso_lease_until";
