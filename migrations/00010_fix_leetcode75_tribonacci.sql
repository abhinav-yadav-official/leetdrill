-- +goose Up
-- +goose StatementBegin

WITH item(list_slug, position, section, leetcode_slug) AS (
  VALUES ('leetcode-75', 59, 'DP - 1D', 'n-th-tribonacci-number')
)
INSERT INTO problem_list_items (list_id, position, section, problem_id)
SELECT pl.id, item.position, item.section, p.id
FROM item
JOIN problem_lists pl ON pl.slug = item.list_slug
JOIN problems p ON p.leetcode_slug = item.leetcode_slug
ON CONFLICT (list_id, position) DO UPDATE SET
    section = EXCLUDED.section,
    problem_id = EXCLUDED.problem_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM problem_list_items pli
USING problem_lists pl, problems p
WHERE pli.list_id = pl.id
  AND pli.problem_id = p.id
  AND pl.slug = 'leetcode-75'
  AND pli.position = 59
  AND p.leetcode_slug = 'n-th-tribonacci-number';
-- +goose StatementEnd
