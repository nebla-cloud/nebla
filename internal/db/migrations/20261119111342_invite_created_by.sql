-- +goose Up
-- drop index "invites_project_id_idx" from table: "invites"
DROP INDEX "invites_project_id_idx";
-- modify "invites" table
ALTER TABLE "invites" ADD COLUMN "created_by" bigint NOT NULL, ADD CONSTRAINT "invites_users_creator" FOREIGN KEY ("created_by") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- create index "invites_project_id_created_by_idx" to table: "invites"
CREATE INDEX "invites_project_id_created_by_idx" ON "invites" ("project_id", "created_by");

-- +goose Down
-- reverse: create index "invites_project_id_created_by_idx" to table: "invites"
DROP INDEX "invites_project_id_created_by_idx";
-- reverse: modify "invites" table
ALTER TABLE "invites" DROP CONSTRAINT "invites_users_creator", DROP COLUMN "created_by";
-- reverse: drop index "invites_project_id_idx" from table: "invites"
CREATE INDEX "invites_project_id_idx" ON "invites" ("project_id");
