# Preserve app-owned class prefixes so the crash frame allowlist remains
# effective after R8; method symbols are still resolved by the uploaded mapping.
-keepnames class io.kombify.speechkit.**
-keepnames class helium314.keyboard.**
