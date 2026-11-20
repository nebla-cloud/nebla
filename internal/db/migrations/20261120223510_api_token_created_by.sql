-- +goose Up
-- modify "api_tokens" table
ALTER TABLE "api_tokens" ADD COLUMN "created_by" bigint NULL, ADD CONSTRAINT "api_tokens_users_creator" FOREIGN KEY ("created_by") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- create index "api_tokens_created_by_idx" to table: "api_tokens"
CREATE INDEX "api_tokens_created_by_idx" ON "api_tokens" ("created_by");

-- +goose Down
-- reverse: create index "api_tokens_created_by_idx" to table: "api_tokens"
DROP INDEX "api_tokens_created_by_idx";
-- reverse: modify "api_tokens" table
ALTER TABLE "api_tokens" DROP CONSTRAINT "api_tokens_users_creator", DROP COLUMN "created_by";
