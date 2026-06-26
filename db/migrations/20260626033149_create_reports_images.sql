-- +goose Up
CREATE TABLE reports_images(
    id UUID PRIMARY KEY,
    report_id UUID REFERENCES reports(id),
    image_objectkey TEXT NOT NULL
);

-- +goose Down
DROP TABLE reports_images;
