-- Add pinned column to quick_notes (default false = 0)
ALTER TABLE quick_notes ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
