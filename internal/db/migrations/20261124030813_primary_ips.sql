-- +goose Up
-- modify "ip_addresses" table
ALTER TABLE "ip_addresses" ADD COLUMN "auto_delete" boolean NOT NULL DEFAULT true;
-- create index "ip_addresses_server_family_primary_key" to table: "ip_addresses"
CREATE UNIQUE INDEX "ip_addresses_server_family_primary_key" ON "ip_addresses" ("server_id", "family") WHERE (((kind)::text = 'primary'::text) AND (server_id IS NOT NULL));
-- modify "servers" table
ALTER TABLE "servers" ADD COLUMN "net_refresh" boolean NOT NULL DEFAULT false, ADD COLUMN "iso_net_hash" text NOT NULL DEFAULT '', ADD COLUMN "iso_lease_until" timestamptz NULL, ADD COLUMN "instance_bump_pending" boolean NOT NULL DEFAULT false;

-- +goose Down
-- reverse: modify "servers" table
ALTER TABLE "servers" DROP COLUMN "instance_bump_pending", DROP COLUMN "iso_lease_until", DROP COLUMN "iso_net_hash", DROP COLUMN "net_refresh";
-- reverse: create index "ip_addresses_server_family_primary_key" to table: "ip_addresses"
DROP INDEX "ip_addresses_server_family_primary_key";
-- reverse: modify "ip_addresses" table
ALTER TABLE "ip_addresses" DROP COLUMN "auto_delete";
