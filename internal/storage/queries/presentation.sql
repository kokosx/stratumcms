-- name: GetSitePresentation :one
SELECT id, active_theme, styles_json, custom_css, version, updated_at FROM site_presentation WHERE id = 1;

-- name: UpdateSitePresentation :execrows
UPDATE site_presentation
SET active_theme = ?, styles_json = ?, custom_css = ?, version = version + 1, updated_at = ?
WHERE id = 1 AND version = ?;
