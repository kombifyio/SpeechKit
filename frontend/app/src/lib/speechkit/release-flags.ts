export type ReleaseFeatureFlags = {
  overlayFocusOption: boolean;
  overlayKombifyDesignOption: boolean;
};

function envFlag(name: string) {
  return import.meta.env[name] === "1" || import.meta.env[name] === "true";
}

export const releaseFeatureFlags: ReleaseFeatureFlags = {
  overlayFocusOption: envFlag("VITE_SPEECHKIT_ENABLE_OVERLAY_FOCUS"),
  overlayKombifyDesignOption: envFlag(
    "VITE_SPEECHKIT_ENABLE_OVERLAY_KOMBIFY_DESIGN",
  ),
};
