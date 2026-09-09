package io.kombify.speechkit.coinstall.v1;

parcelable ProvisionResult {
    String serverUrl;
    String bearerToken;
    String subject;
    // Milliseconds since epoch. Zero means the callee did not pin an expiry;
    // the caller may use a local TTL. Additive on v1: unread by older callers.
    long expiresAtEpochMs;
}
