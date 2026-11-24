-- name: ListTransferLinks :many
-- All links with both lines' details (GET /api/transfers, transfer review page).
SELECT tl.id, tl.out_line_id, tl.in_line_id, tl.method, tl.created_at,
       o.account_id AS out_account_id, oa.name AS out_account_name,
       o.booking_date AS out_booking_date, o.message AS out_message, o.amount_minor AS out_amount_minor,
       i.account_id AS in_account_id, ia.name AS in_account_name,
       i.booking_date AS in_booking_date, i.message AS in_message, i.amount_minor AS in_amount_minor
FROM transfer_link tl
JOIN statement_line o ON o.id = tl.out_line_id
JOIN statement_line i ON i.id = tl.in_line_id
JOIN account oa ON oa.id = o.account_id
JOIN account ia ON ia.id = i.account_id
ORDER BY tl.id DESC;
