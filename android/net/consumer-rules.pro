# Keep Moshi-generated JsonAdapters for the wire frame types. Moshi resolves
# generated adapters reflectively by class name, so R8 must not rename them.
-keep class io.kombify.speechkit.net.**JsonAdapter { *; }
-keepnames class io.kombify.speechkit.net.** { *; }
