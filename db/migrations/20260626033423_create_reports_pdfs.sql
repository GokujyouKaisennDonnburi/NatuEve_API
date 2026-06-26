-- +goose Up
CREATE TABLE reports_pdfs(
    id UUID PRIMARY KEY,
    report_id UUID REFERENCES reports(id),
    pdf_objectkey TEXT NOT NULL
);

-- +goose Down
DROP TABLE reports_pdfs;