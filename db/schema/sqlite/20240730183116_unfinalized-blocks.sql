-- +goose Up
-- +goose StatementBegin

ALTER TABLE "unfinalized_blocks"
ADD "status" integer NOT NULL DEFAULT 0;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
SELECT 'NOT SUPPORTED';
-- +goose StatementEnd
