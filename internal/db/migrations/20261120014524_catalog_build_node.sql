-- +goose Up
-- modify "catalog_builds" table
ALTER TABLE "catalog_builds" ADD COLUMN "build_node" text NULL;

-- +goose Down
-- reverse: modify "catalog_builds" table
ALTER TABLE "catalog_builds" DROP COLUMN "build_node";
