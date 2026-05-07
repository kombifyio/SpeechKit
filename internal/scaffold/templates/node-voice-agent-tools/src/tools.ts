// Host-side tools the Voice Agent may invoke. The Voice Agent runtime
// declares them on the start frame's `system_prompt_override` (or via the
// persona's role) and emits a `tool_call` frame when it wants one
// executed.

export interface ToolHandler {
  /** Schema is JSON-Schema; use it to document the tool to the model. */
  schema: {
    name: string;
    description: string;
    parameters: Record<string, unknown>;
  };
  invoke(args: Record<string, unknown>): Promise<Record<string, unknown>>;
}

export const tools: Record<string, ToolHandler> = {
  getCurrentWeather: {
    schema: {
      name: "getCurrentWeather",
      description: "Return the current weather for a city. Demo stub.",
      parameters: {
        type: "object",
        properties: {
          city: { type: "string", description: "City name, e.g. Berlin." },
        },
        required: ["city"],
      },
    },
    async invoke(args) {
      const city = String(args["city"] ?? "Berlin");
      // In production, call your weather provider here.
      return {
        city,
        temperature_c: 14 + Math.floor(Math.random() * 8),
        conditions: "partly cloudy",
        source: "{{ .APP_NAME }}-stub",
      };
    },
  },
};
