import type { SpeechKitVoiceEvent } from "./voice-surface.js";

/**
 * Pure, headless teleprompter turn reducer for the voice_agent dialogue view
 * (Voice Chat UI Standard, AI-VOICE-SPEECHKIT-TARGET.md). 1:1 port of the
 * Floating Panel reference implementation (`voice-agent-turns.ts`); the rules
 * below are also the porting specification for the Compose implementation.
 *
 * Turn-building rules (input: canonical `speechkit.voice_surface.v1` events):
 *
 * - `voice.transcript_draft` (user speech, streaming): `text` carries the
 *   CUMULATIVE text of the current user turn. It replaces the text of the open
 *   user turn; when no user turn is open, a new draft turn is appended.
 *   Events without text open no turn.
 * - `voice.transcript_final` (user turn done): closes the open user turn with
 *   the final text (`final: true`). When the event text is empty the existing
 *   draft text is kept. Without an open turn, a non-empty final appends a
 *   closed turn directly.
 * - `voice.agent_turn` with `final: false` (agent speech, streaming): the
 *   canonical streaming form of agent output. `output_text` carries the
 *   CUMULATIVE text of the current agent turn and replaces the open agent
 *   turn's text (appending a new draft turn when none is open).
 * - `voice.agent_turn` with `final: true`: closes the open agent turn with the
 *   cumulative text.
 * - `voice.barge_in`: the user interrupted agent playback. The open agent turn
 *   is marked `interrupted: true` AND closed (`final: true`); the next
 *   `voice.agent_turn` starts a fresh turn. Without an open agent turn the
 *   event is a no-op.
 * - Every other event type leaves the turn list untouched and returns the
 *   SAME array reference (cheap change detection for reactive consumers).
 *
 * "Open turn" = the last turn of the given role with `final: false`. Renderers
 * dim turns with `final: false` (drafts) and show closed turns solid; user
 * turns are visually secondary, agent turns primary.
 */
export interface VoiceAgentTurn {
  role: "user" | "agent";
  text: string;
  /** false = streaming draft (render dimmed); true = closed turn (solid). */
  final: boolean;
  /** true when the turn was cut short by a barge-in (always closed). */
  interrupted: boolean;
}

export function reduceVoiceAgentTurns(
  turns: readonly VoiceAgentTurn[],
  event: SpeechKitVoiceEvent
): VoiceAgentTurn[] {
  switch (event.type) {
    case "voice.transcript_draft":
      return upsertDraft(turns, "user", event.text ?? "");
    case "voice.transcript_final":
      return closeTurn(turns, "user", event.text ?? "");
    case "voice.agent_turn":
      return event.final === true
        ? closeTurn(turns, "agent", event.output_text ?? "")
        : upsertDraft(turns, "agent", event.output_text ?? "");
    case "voice.barge_in":
      return interruptOpenTurn(turns, "agent");
    default:
      return turns as VoiceAgentTurn[];
  }
}

function openTurnIndex(turns: readonly VoiceAgentTurn[], role: VoiceAgentTurn["role"]): number {
  for (let i = turns.length - 1; i >= 0; i -= 1) {
    const turn = turns[i];
    if (turn !== undefined && turn.role === role && !turn.final) return i;
  }
  return -1;
}

function upsertDraft(
  turns: readonly VoiceAgentTurn[],
  role: VoiceAgentTurn["role"],
  text: string
): VoiceAgentTurn[] {
  const index = openTurnIndex(turns, role);
  const open = index === -1 ? undefined : turns[index];
  if (open === undefined) {
    if (!text) return turns as VoiceAgentTurn[];
    return [...turns, { role, text, final: false, interrupted: false }];
  }
  return replaceTurn(turns, index, { ...open, text });
}

function closeTurn(
  turns: readonly VoiceAgentTurn[],
  role: VoiceAgentTurn["role"],
  text: string
): VoiceAgentTurn[] {
  const index = openTurnIndex(turns, role);
  const open = index === -1 ? undefined : turns[index];
  if (open === undefined) {
    if (!text) return turns as VoiceAgentTurn[];
    return [...turns, { role, text, final: true, interrupted: false }];
  }
  return replaceTurn(turns, index, {
    ...open,
    text: text || open.text,
    final: true
  });
}

function interruptOpenTurn(
  turns: readonly VoiceAgentTurn[],
  role: VoiceAgentTurn["role"]
): VoiceAgentTurn[] {
  const index = openTurnIndex(turns, role);
  const open = index === -1 ? undefined : turns[index];
  if (open === undefined) return turns as VoiceAgentTurn[];
  return replaceTurn(turns, index, { ...open, final: true, interrupted: true });
}

function replaceTurn(
  turns: readonly VoiceAgentTurn[],
  index: number,
  turn: VoiceAgentTurn
): VoiceAgentTurn[] {
  const next = [...turns];
  next[index] = turn;
  return next;
}
