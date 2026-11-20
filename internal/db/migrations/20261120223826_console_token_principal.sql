-- +goose Up
-- modify "console_tokens" table
ALTER TABLE "console_tokens" ADD COLUMN "user_id" bigint NULL, ADD COLUMN "session_hash" bytea NULL, ADD COLUMN "api_token_id" bigint NULL;

-- +goose Down
-- reverse: modify "console_tokens" table
ALTER TABLE "console_tokens" DROP COLUMN "api_token_id", DROP COLUMN "session_hash", DROP COLUMN "user_id";
