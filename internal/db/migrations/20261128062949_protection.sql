-- +goose Up
-- modify "ip_addresses" table
ALTER TABLE "ip_addresses" ADD COLUMN "delete_protected" boolean NOT NULL DEFAULT false;
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "delete_protected" boolean NOT NULL DEFAULT false, ADD COLUMN "rebuild_protected" boolean NOT NULL DEFAULT false;
-- modify "snapshots" table
ALTER TABLE "snapshots" ADD COLUMN "delete_protected" boolean NOT NULL DEFAULT false;
-- modify "volumes" table
ALTER TABLE "volumes" ADD COLUMN "delete_protected" boolean NOT NULL DEFAULT false;

-- +goose Down
-- reverse: modify "volumes" table
ALTER TABLE "volumes" DROP COLUMN "delete_protected";
-- reverse: modify "snapshots" table
ALTER TABLE "snapshots" DROP COLUMN "delete_protected";
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "rebuild_protected", DROP COLUMN "delete_protected";
-- reverse: modify "ip_addresses" table
ALTER TABLE "ip_addresses" DROP COLUMN "delete_protected";
