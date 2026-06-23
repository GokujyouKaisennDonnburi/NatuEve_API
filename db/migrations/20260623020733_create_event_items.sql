-- +goose Up
CREATE TABLE event_items(
	id UUID PRIMARY KEY,
	event_id    UUID REFERENCES events(id),
	event_item  VARCHAR(255) NOT NULL,
	is_required BOOLEAN NOT NULL DEFAULT FALSE
);

-- +goose Down
DROP TABLE event_items;
