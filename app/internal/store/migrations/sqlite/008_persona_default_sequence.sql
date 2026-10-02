-- Migration 008: add default sequence selection to Voice Agent personas.

ALTER TABLE voice_agent_personas
ADD COLUMN default_sequence TEXT NOT NULL DEFAULT '';
