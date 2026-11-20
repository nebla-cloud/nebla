-- +goose Up
-- modify "projects" table
ALTER TABLE "projects" ADD COLUMN "created_by" bigint NULL, ADD CONSTRAINT "projects_users_creator" FOREIGN KEY ("created_by") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- create index "projects_created_by_idx" to table: "projects"
CREATE INDEX "projects_created_by_idx" ON "projects" ("created_by");

-- +goose Down
-- reverse: create index "projects_created_by_idx" to table: "projects"
DROP INDEX "projects_created_by_idx";
-- reverse: modify "projects" table
ALTER TABLE "projects" DROP CONSTRAINT "projects_users_creator", DROP COLUMN "created_by";
