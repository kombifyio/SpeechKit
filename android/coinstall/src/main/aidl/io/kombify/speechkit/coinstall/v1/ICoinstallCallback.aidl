package io.kombify.speechkit.coinstall.v1;

oneway interface ICoinstallCallback {
    void onPartial(String turnId, String text);
    void onComplete(String turnId, String text);
    void onError(String turnId, int code, String message);
}
