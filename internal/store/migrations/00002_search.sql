-- +goose Up
-- Full-text index over card titles, descriptions, comments and checklist
-- items. remove_diacritics lets "kowhai" match "Kōwhai".
CREATE VIRTUAL TABLE search_index USING fts5 (
    title,
    body,
    entity_type UNINDEXED,
    entity_id UNINDEXED,
    card_id UNINDEXED,
    board_id UNINDEXED,
    tokenize = 'unicode61 remove_diacritics 2',
    prefix = '2 3'
);

-- +goose Down
DROP TABLE search_index;
