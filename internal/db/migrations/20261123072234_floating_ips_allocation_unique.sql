-- +goose Up
-- create index "ip_addresses_allocation_id_key" to table: "ip_addresses"
CREATE UNIQUE INDEX "ip_addresses_allocation_id_key" ON "ip_addresses" ("allocation_id");

-- +goose Down
-- reverse: create index "ip_addresses_allocation_id_key" to table: "ip_addresses"
DROP INDEX "ip_addresses_allocation_id_key";
