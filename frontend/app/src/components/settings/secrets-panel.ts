import type { ProviderCredentialState } from "@/lib/speechkit";

export function providerSecretNoun(provider?: string) {
  return provider === "huggingface" ? "token" : "key";
}

export function providerCredentialCopy(
  profileName: string,
  credential: ProviderCredentialState,
) {
  const noun = providerSecretNoun(credential.provider);
  const credentialLabel = `${credential.label} ${noun}`;
  return {
    title: `Add ${credentialLabel}`,
    inputLabel: `${profileName} ${credentialLabel}`,
    placeholder: credential.envName || (noun === "token" ? "Token" : "API key"),
    saveLabel: `Save ${noun}`,
    neededLabel: `${credentialLabel} needed`,
    unlockLabel: `Add the ${noun} above to unlock this model.`,
  };
}
