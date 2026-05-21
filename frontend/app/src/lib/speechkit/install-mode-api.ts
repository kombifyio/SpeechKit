export type InstallModeRequest = "local" | "cloud" | "server";

/**
 * POSTs the install-mode the user picked during onboarding so the
 * backend can flip installState.Mode away from Local. Failures are
 * non-fatal — the wizard's complete-setup call will surface any real
 * problems; this helper logs and returns.
 */
export async function postInstallMode(mode: InstallModeRequest): Promise<void> {
  try {
    const resp = await fetch("/app/install-mode", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode }),
    });
    if (!resp.ok) {
      const text = await resp.text();
      console.warn(`install-mode POST ${resp.status}: ${text}`);
    }
  } catch (err) {
    console.warn("install-mode POST failed:", err);
  }
}
