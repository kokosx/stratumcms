CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX entries_author_type_idx ON entries(author_id, content_type_id, updated_at DESC);
CREATE INDEX media_created_at_idx ON media(created_at DESC);
CREATE UNIQUE INDEX menu_items_menu_position_idx ON menu_items(menu_id, position);
