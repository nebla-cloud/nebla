-- +goose Up
-- create "console_tokens" table
CREATE TABLE "console_tokens" (
  "token_hash" bytea NOT NULL,
  "project_id" bigint NOT NULL,
  "vmid" bigint NOT NULL,
  "ticket" text NOT NULL,
  "port" bigint NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "server_id" bigint NOT NULL,
  PRIMARY KEY ("token_hash"),
  CONSTRAINT "console_tokens_servers_server" FOREIGN KEY ("server_id") REFERENCES "servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- create index "console_tokens_expires_at_idx" to table: "console_tokens"
CREATE INDEX "console_tokens_expires_at_idx" ON "console_tokens" ("expires_at");
-- create index "console_tokens_server_id_idx" to table: "console_tokens"
CREATE INDEX "console_tokens_server_id_idx" ON "console_tokens" ("server_id");

-- +goose Down
-- reverse: create index "console_tokens_server_id_idx" to table: "console_tokens"
DROP INDEX "console_tokens_server_id_idx";
-- reverse: create index "console_tokens_expires_at_idx" to table: "console_tokens"
DROP INDEX "console_tokens_expires_at_idx";
-- reverse: create "console_tokens" table
DROP TABLE "console_tokens";
