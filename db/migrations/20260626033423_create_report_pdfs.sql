-- +goose Up
CREATE TABLE report_pdfs(
    id UUID PRIMARY KEY,
    report_id UUID REFERENCES reports(id),
    pdf_objectkey TEXT NOT NULL
);

-- +goose Down
DROP TABLE report_pdfs;