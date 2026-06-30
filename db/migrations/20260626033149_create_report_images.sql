-- +goose Up
CREATE TABLE report_images(
    id UUID PRIMARY KEY,
    report_id UUID REFERENCES reports(id),
    image_objectkey TEXT NOT NULL
);

-- +goose Down
DROP TABLE report_images;
