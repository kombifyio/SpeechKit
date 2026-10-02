/**
 * Message-ID catalogs for the Voice UI Kit (`sk.voice.*`). English is the
 * source of truth and fallback; the Wave 2 locale set is en, de, es, zh-Hans,
 * hi, and ar (LANGUAGE-LOCALIZATION-STANDARD). Seeded from the Floating Panel
 * `fp.voice.*` catalogs (machine-drafted, human review pending).
 *
 * The JSON copies in `locales/*.json` ship for native (Compose) spec parity —
 * `test/i18n-json-sync.test.ts` keeps them byte-equal with these catalogs.
 */

export const VOICE_UI_LOCALES = ["en", "de", "es", "zh-Hans", "hi", "ar"] as const;
export type VoiceUiLocale = (typeof VOICE_UI_LOCALES)[number];

export type VoiceUiMessageId =
  | "sk.voice.button.dictation.start"
  | "sk.voice.button.dictation.stop"
  | "sk.voice.button.cancel"
  | "sk.voice.button.agent"
  | "sk.voice.button.agent_locked"
  | "sk.voice.state.idle"
  | "sk.voice.state.capturing"
  | "sk.voice.state.processing"
  | "sk.voice.state.speaking"
  | "sk.voice.state.cancelled"
  | "sk.voice.state.denied"
  | "sk.voice.agent.connecting"
  | "sk.voice.agent.listening"
  | "sk.voice.agent.speaking"
  | "sk.voice.agent.interrupt"
  | "sk.voice.agent.interrupted"
  | "sk.voice.agent.you"
  | "sk.voice.agent.assistant"
  | "sk.voice.agent.live"
  | "sk.voice.agent.jumpToLive"
  | "sk.voice.agent.ended"
  | "sk.voice.agent.reconnect"
  | "sk.voice.agent.exit"
  | "sk.voice.denied.retry"
  | "sk.voice.consent.title"
  | "sk.voice.consent.capture"
  | "sk.voice.consent.destination"
  | "sk.voice.consent.accept"
  | "sk.voice.consent.decline"
  | "sk.voice.consent.declined"
  | "sk.voice.consent.continuous"
  | "sk.voice.state.requesting"
  | "sk.voice.notice.mic_denied"
  | "sk.voice.notice.mic_denied.hint"
  | "sk.voice.notice.mic_unavailable"
  | "sk.voice.notice.mic_unavailable.hint"
  | "sk.voice.notice.mic_busy"
  | "sk.voice.notice.mic_busy.hint"
  | "sk.voice.notice.mic_unsupported"
  | "sk.voice.notice.mic_unsupported.hint"
  | "sk.voice.notice.connection"
  | "sk.voice.notice.connection.hint"
  | "sk.voice.notice.not_available"
  | "sk.voice.notice.not_available.hint"
  | "sk.voice.notice.quota"
  | "sk.voice.notice.quota.hint"
  | "sk.voice.notice.consent"
  | "sk.voice.notice.consent.hint"
  | "sk.voice.notice.generic"
  | "sk.voice.notice.generic.hint"
  | "sk.voice.notice.details"
  | "sk.voice.notice.hide_details"
  | "sk.voice.notice.dismiss"
  | "sk.voice.notice.code"
  | "sk.voice.notice.reference"
  | "sk.voice.dialog.label"
  | "sk.voice.dialog.end"
  | "sk.voice.dialog.hide"
  | "sk.voice.transcript.label";

export type VoiceUiMessageCatalog = Readonly<Record<VoiceUiMessageId, string>>;

export const en: VoiceUiMessageCatalog = {
  "sk.voice.button.dictation.start": "Start voice dictation",
  "sk.voice.button.dictation.stop": "Stop recording",
  "sk.voice.button.cancel": "Cancel voice input",
  "sk.voice.button.agent": "Switch to voice conversation",
  "sk.voice.button.agent_locked": "Voice conversation (locked)",
  "sk.voice.state.idle": "Ready",
  "sk.voice.state.capturing": "Listening",
  "sk.voice.state.processing": "Processing",
  "sk.voice.state.speaking": "Speaking",
  "sk.voice.state.cancelled": "Cancelled",
  "sk.voice.state.denied": "Unavailable",
  "sk.voice.agent.connecting": "Connecting",
  "sk.voice.agent.listening": "Listening",
  "sk.voice.agent.speaking": "Speaking",
  "sk.voice.agent.interrupt": "Tap to interrupt",
  "sk.voice.agent.interrupted": "Interrupted",
  "sk.voice.agent.you": "You",
  "sk.voice.agent.assistant": "Assistant",
  "sk.voice.agent.live": "Live",
  "sk.voice.agent.jumpToLive": "Jump to live",
  "sk.voice.agent.ended": "The voice session has ended.",
  "sk.voice.agent.reconnect": "Reconnect",
  "sk.voice.agent.exit": "Exit voice mode",
  "sk.voice.denied.retry": "Try again",
  "sk.voice.consent.title": "Use voice with kombify hosted processing?",
  "sk.voice.consent.capture": "What is captured: your microphone audio, only while you record.",
  "sk.voice.consent.destination":
    "Where it goes: kombify hosted voice at api.kombify.io, to transcribe and process your speech.",
  "sk.voice.consent.accept": "Accept and enable voice",
  "sk.voice.consent.decline": "Decline",
  "sk.voice.consent.declined": "Voice input is off: you declined voice capture on this surface.",
  "sk.voice.consent.continuous":
    "Voice conversation streams your microphone continuously to kombify hosted voice (api.kombify.io) for the entire session — not only while you press record. Streaming stops when you exit voice mode.",
  "sk.voice.state.requesting": "Waiting for microphone",
  "sk.voice.notice.mic_denied": "Microphone is blocked",
  "sk.voice.notice.mic_denied.hint": "Allow microphone access for this site in the browser's address bar or settings, then try again.",
  "sk.voice.notice.mic_unavailable": "No microphone found",
  "sk.voice.notice.mic_unavailable.hint": "Connect a microphone or pick another input device, then try again.",
  "sk.voice.notice.mic_busy": "Microphone is in use",
  "sk.voice.notice.mic_busy.hint": "Close the other app that uses the microphone, then try again.",
  "sk.voice.notice.mic_unsupported": "Voice input not supported",
  "sk.voice.notice.mic_unsupported.hint": "This browser or page can't capture audio. Use a current browser on a secure (HTTPS) page.",
  "sk.voice.notice.connection": "Couldn't connect to voice",
  "sk.voice.notice.connection.hint": "Check your connection and try again.",
  "sk.voice.notice.not_available": "Voice isn't available here",
  "sk.voice.notice.not_available.hint": "This voice feature isn't enabled for your account or workspace.",
  "sk.voice.notice.quota": "Voice limit reached",
  "sk.voice.notice.quota.hint": "The usage limit for voice has been reached. Try again later.",
  "sk.voice.notice.consent": "Voice needs confirmation",
  "sk.voice.notice.consent.hint": "Confirm voice capture once to continue; you can revoke it at any time.",
  "sk.voice.notice.generic": "Voice stopped unexpectedly",
  "sk.voice.notice.generic.hint": "Try again. If it keeps happening, share the reference below with support.",
  "sk.voice.notice.details": "Details",
  "sk.voice.notice.hide_details": "Hide details",
  "sk.voice.notice.dismiss": "Dismiss",
  "sk.voice.notice.code": "Code",
  "sk.voice.notice.reference": "Reference",
  "sk.voice.dialog.label": "Voice conversation",
  "sk.voice.dialog.end": "End conversation",
  "sk.voice.dialog.hide": "Hide conversation",
  "sk.voice.transcript.label": "Live transcript"
};

export const de: VoiceUiMessageCatalog = {
  "sk.voice.button.dictation.start": "Sprachdiktat starten",
  "sk.voice.button.dictation.stop": "Aufnahme beenden",
  "sk.voice.button.cancel": "Spracheingabe abbrechen",
  "sk.voice.button.agent": "Zur Sprachkonversation wechseln",
  "sk.voice.button.agent_locked": "Sprachkonversation (gesperrt)",
  "sk.voice.state.idle": "Bereit",
  "sk.voice.state.capturing": "Hört zu",
  "sk.voice.state.processing": "Verarbeitet",
  "sk.voice.state.speaking": "Antwortet",
  "sk.voice.state.cancelled": "Abgebrochen",
  "sk.voice.state.denied": "Nicht verfügbar",
  "sk.voice.agent.connecting": "Verbindet",
  "sk.voice.agent.listening": "Hört zu",
  "sk.voice.agent.speaking": "Spricht",
  "sk.voice.agent.interrupt": "Tippen zum Unterbrechen",
  "sk.voice.agent.interrupted": "Unterbrochen",
  "sk.voice.agent.you": "Du",
  "sk.voice.agent.assistant": "Assistent",
  "sk.voice.agent.live": "Live",
  "sk.voice.agent.jumpToLive": "Zum Live-Ende springen",
  "sk.voice.agent.ended": "Die Sprachsitzung wurde beendet.",
  "sk.voice.agent.reconnect": "Erneut verbinden",
  "sk.voice.agent.exit": "Sprachmodus beenden",
  "sk.voice.denied.retry": "Erneut versuchen",
  "sk.voice.consent.title": "Sprache mit kombify Hosted-Verarbeitung nutzen?",
  "sk.voice.consent.capture": "Was erfasst wird: dein Mikrofon-Audio, nur während der Aufnahme.",
  "sk.voice.consent.destination":
    "Wohin es geht: kombify Hosted Voice unter api.kombify.io, um deine Sprache zu transkribieren und zu verarbeiten.",
  "sk.voice.consent.accept": "Akzeptieren und Sprache aktivieren",
  "sk.voice.consent.decline": "Ablehnen",
  "sk.voice.consent.declined":
    "Spracheingabe ist aus: du hast die Sprachaufnahme auf dieser Oberfläche abgelehnt.",
  "sk.voice.consent.continuous":
    "Die Sprachkonversation streamt dein Mikrofon während der gesamten Sitzung fortlaufend an kombify Hosted Voice (api.kombify.io) — nicht nur während du aufnimmst. Das Streaming endet, sobald du den Sprachmodus verlässt.",
  "sk.voice.state.requesting": "Warte auf Mikrofon",
  "sk.voice.notice.mic_denied": "Mikrofon ist blockiert",
  "sk.voice.notice.mic_denied.hint": "Erlaube den Mikrofonzugriff für diese Seite in der Adressleiste oder den Browser-Einstellungen und versuche es erneut.",
  "sk.voice.notice.mic_unavailable": "Kein Mikrofon gefunden",
  "sk.voice.notice.mic_unavailable.hint": "Schließe ein Mikrofon an oder wähle ein anderes Eingabegerät und versuche es erneut.",
  "sk.voice.notice.mic_busy": "Mikrofon wird verwendet",
  "sk.voice.notice.mic_busy.hint": "Schließe die andere App, die das Mikrofon nutzt, und versuche es erneut.",
  "sk.voice.notice.mic_unsupported": "Spracheingabe nicht unterstützt",
  "sk.voice.notice.mic_unsupported.hint": "Dieser Browser oder diese Seite kann kein Audio aufnehmen. Nutze einen aktuellen Browser auf einer sicheren (HTTPS-)Seite.",
  "sk.voice.notice.connection": "Sprache nicht erreichbar",
  "sk.voice.notice.connection.hint": "Prüfe deine Verbindung und versuche es erneut.",
  "sk.voice.notice.not_available": "Sprache hier nicht verfügbar",
  "sk.voice.notice.not_available.hint": "Diese Sprachfunktion ist für dein Konto oder deinen Workspace nicht aktiviert.",
  "sk.voice.notice.quota": "Sprachlimit erreicht",
  "sk.voice.notice.quota.hint": "Das Nutzungslimit für Sprache ist erreicht. Versuche es später erneut.",
  "sk.voice.notice.consent": "Sprache braucht Bestätigung",
  "sk.voice.notice.consent.hint": "Bestätige die Sprachaufnahme einmalig, um fortzufahren; du kannst sie jederzeit widerrufen.",
  "sk.voice.notice.generic": "Sprache unerwartet beendet",
  "sk.voice.notice.generic.hint": "Versuche es erneut. Wenn es weiter passiert, gib dem Support die Referenz unten.",
  "sk.voice.notice.details": "Details",
  "sk.voice.notice.hide_details": "Details ausblenden",
  "sk.voice.notice.dismiss": "Schließen",
  "sk.voice.notice.code": "Code",
  "sk.voice.notice.reference": "Referenz",
  "sk.voice.dialog.label": "Sprachkonversation",
  "sk.voice.dialog.end": "Konversation beenden",
  "sk.voice.dialog.hide": "Konversation ausblenden",
  "sk.voice.transcript.label": "Live-Transkript"
};

export const es: VoiceUiMessageCatalog = {
  "sk.voice.button.dictation.start": "Iniciar dictado por voz",
  "sk.voice.button.dictation.stop": "Detener la grabación",
  "sk.voice.button.cancel": "Cancelar la entrada de voz",
  "sk.voice.button.agent": "Cambiar a conversación por voz",
  "sk.voice.button.agent_locked": "Conversación por voz (bloqueada)",
  "sk.voice.state.idle": "Listo",
  "sk.voice.state.capturing": "Escuchando",
  "sk.voice.state.processing": "Procesando",
  "sk.voice.state.speaking": "Hablando",
  "sk.voice.state.cancelled": "Cancelado",
  "sk.voice.state.denied": "No disponible",
  "sk.voice.agent.connecting": "Conectando",
  "sk.voice.agent.listening": "Escuchando",
  "sk.voice.agent.speaking": "Hablando",
  "sk.voice.agent.interrupt": "Toca para interrumpir",
  "sk.voice.agent.interrupted": "Interrumpido",
  "sk.voice.agent.you": "Tú",
  "sk.voice.agent.assistant": "Asistente",
  "sk.voice.agent.live": "En vivo",
  "sk.voice.agent.jumpToLive": "Ir al directo",
  "sk.voice.agent.ended": "La sesión de voz ha terminado.",
  "sk.voice.agent.reconnect": "Reconectar",
  "sk.voice.agent.exit": "Salir del modo de voz",
  "sk.voice.denied.retry": "Intentar de nuevo",
  "sk.voice.consent.title": "¿Usar voz con el procesamiento alojado de kombify?",
  "sk.voice.consent.capture": "Qué se captura: el audio de tu micrófono, solo mientras grabas.",
  "sk.voice.consent.destination":
    "Adónde va: a la voz alojada de kombify en api.kombify.io, para transcribir y procesar tu voz.",
  "sk.voice.consent.accept": "Aceptar y activar la voz",
  "sk.voice.consent.decline": "Rechazar",
  "sk.voice.consent.declined":
    "La entrada de voz está desactivada: rechazaste la captura de voz en esta superficie.",
  "sk.voice.consent.continuous":
    "La conversación por voz transmite tu micrófono de forma continua a la voz alojada de kombify (api.kombify.io) durante toda la sesión, no solo mientras grabas. La transmisión se detiene al salir del modo de voz.",
  "sk.voice.state.requesting": "Esperando el micrófono",
  "sk.voice.notice.mic_denied": "Micrófono bloqueado",
  "sk.voice.notice.mic_denied.hint": "Permite el acceso al micrófono para este sitio en la barra de direcciones o en la configuración del navegador y vuelve a intentarlo.",
  "sk.voice.notice.mic_unavailable": "No se encontró ningún micrófono",
  "sk.voice.notice.mic_unavailable.hint": "Conecta un micrófono o elige otro dispositivo de entrada y vuelve a intentarlo.",
  "sk.voice.notice.mic_busy": "El micrófono está en uso",
  "sk.voice.notice.mic_busy.hint": "Cierra la otra aplicación que usa el micrófono y vuelve a intentarlo.",
  "sk.voice.notice.mic_unsupported": "La entrada de voz no es compatible aquí",
  "sk.voice.notice.mic_unsupported.hint": "Este navegador o esta página no puede capturar audio. Usa un navegador actual en una página segura (HTTPS).",
  "sk.voice.notice.connection": "No se pudo conectar con la voz",
  "sk.voice.notice.connection.hint": "Comprueba tu conexión y vuelve a intentarlo.",
  "sk.voice.notice.not_available": "La voz no está disponible aquí",
  "sk.voice.notice.not_available.hint": "Esta función de voz no está activada para tu cuenta o espacio de trabajo.",
  "sk.voice.notice.quota": "Se alcanzó el límite de voz",
  "sk.voice.notice.quota.hint": "Se alcanzó el límite de uso de voz. Vuelve a intentarlo más tarde.",
  "sk.voice.notice.consent": "La voz necesita tu confirmación",
  "sk.voice.notice.consent.hint": "Confirma la captura de voz una vez para continuar; puedes revocarla en cualquier momento.",
  "sk.voice.notice.generic": "La voz se detuvo inesperadamente",
  "sk.voice.notice.generic.hint": "Vuelve a intentarlo. Si sigue ocurriendo, comparte la referencia de abajo con el soporte.",
  "sk.voice.notice.details": "Detalles",
  "sk.voice.notice.hide_details": "Ocultar detalles",
  "sk.voice.notice.dismiss": "Descartar",
  "sk.voice.notice.code": "Código",
  "sk.voice.notice.reference": "Referencia",
  "sk.voice.dialog.label": "Conversación por voz",
  "sk.voice.dialog.end": "Terminar la conversación",
  "sk.voice.dialog.hide": "Ocultar la conversación",
  "sk.voice.transcript.label": "Transcripción en vivo"
};

export const zhHans: VoiceUiMessageCatalog = {
  "sk.voice.button.dictation.start": "开始语音听写",
  "sk.voice.button.dictation.stop": "停止录音",
  "sk.voice.button.cancel": "取消语音输入",
  "sk.voice.button.agent": "切换到语音对话",
  "sk.voice.button.agent_locked": "语音对话（已锁定）",
  "sk.voice.state.idle": "就绪",
  "sk.voice.state.capturing": "正在聆听",
  "sk.voice.state.processing": "正在处理",
  "sk.voice.state.speaking": "正在回答",
  "sk.voice.state.cancelled": "已取消",
  "sk.voice.state.denied": "不可用",
  "sk.voice.agent.connecting": "正在连接",
  "sk.voice.agent.listening": "正在聆听",
  "sk.voice.agent.speaking": "正在回答",
  "sk.voice.agent.interrupt": "点按以打断",
  "sk.voice.agent.interrupted": "已打断",
  "sk.voice.agent.you": "你",
  "sk.voice.agent.assistant": "助手",
  "sk.voice.agent.live": "实时",
  "sk.voice.agent.jumpToLive": "跳转到实时位置",
  "sk.voice.agent.ended": "语音会话已结束。",
  "sk.voice.agent.reconnect": "重新连接",
  "sk.voice.agent.exit": "退出语音模式",
  "sk.voice.denied.retry": "重试",
  "sk.voice.consent.title": "使用 kombify 托管语音处理？",
  "sk.voice.consent.capture": "采集内容：仅在录音期间采集你的麦克风音频。",
  "sk.voice.consent.destination":
    "数据去向：发送到 api.kombify.io 上的 kombify 托管语音服务，用于转写和处理你的语音。",
  "sk.voice.consent.accept": "接受并启用语音",
  "sk.voice.consent.decline": "拒绝",
  "sk.voice.consent.declined": "语音输入已关闭：你在此界面拒绝了语音采集。",
  "sk.voice.consent.continuous":
    "语音对话会在整个会话期间将你的麦克风音频持续传输到 kombify 托管语音服务（api.kombify.io），而不仅是在录音时。退出语音模式后传输即停止。",
  "sk.voice.state.requesting": "正在等待麦克风",
  "sk.voice.notice.mic_denied": "麦克风被阻止",
  "sk.voice.notice.mic_denied.hint": "请在浏览器地址栏或设置中允许此网站访问麦克风，然后重试。",
  "sk.voice.notice.mic_unavailable": "未找到麦克风",
  "sk.voice.notice.mic_unavailable.hint": "请连接麦克风或选择其他输入设备，然后重试。",
  "sk.voice.notice.mic_busy": "麦克风正被占用",
  "sk.voice.notice.mic_busy.hint": "请关闭正在使用麦克风的其他应用，然后重试。",
  "sk.voice.notice.mic_unsupported": "此处不支持语音输入",
  "sk.voice.notice.mic_unsupported.hint": "此浏览器或页面无法采集音频。请在安全（HTTPS）页面上使用最新的浏览器。",
  "sk.voice.notice.connection": "无法连接到语音服务",
  "sk.voice.notice.connection.hint": "请检查网络连接，然后重试。",
  "sk.voice.notice.not_available": "此处无法使用语音",
  "sk.voice.notice.not_available.hint": "你的账户或工作区未启用此语音功能。",
  "sk.voice.notice.quota": "已达到语音使用上限",
  "sk.voice.notice.quota.hint": "语音使用量已达上限，请稍后重试。",
  "sk.voice.notice.consent": "语音需要你的确认",
  "sk.voice.notice.consent.hint": "确认一次语音采集即可继续；你可以随时撤销。",
  "sk.voice.notice.generic": "语音意外停止",
  "sk.voice.notice.generic.hint": "请重试。如果问题持续出现，请将下方的参考编号提供给支持团队。",
  "sk.voice.notice.details": "详情",
  "sk.voice.notice.hide_details": "隐藏详情",
  "sk.voice.notice.dismiss": "关闭",
  "sk.voice.notice.code": "代码",
  "sk.voice.notice.reference": "参考编号",
  "sk.voice.dialog.label": "语音对话",
  "sk.voice.dialog.end": "结束对话",
  "sk.voice.dialog.hide": "隐藏对话",
  "sk.voice.transcript.label": "实时转写"
};

export const hi: VoiceUiMessageCatalog = {
  "sk.voice.button.dictation.start": "वॉइस डिक्टेशन शुरू करें",
  "sk.voice.button.dictation.stop": "रिकॉर्डिंग रोकें",
  "sk.voice.button.cancel": "वॉइस इनपुट रद्द करें",
  "sk.voice.button.agent": "वॉइस वार्तालाप पर जाएँ",
  "sk.voice.button.agent_locked": "वॉइस वार्तालाप (लॉक)",
  "sk.voice.state.idle": "तैयार",
  "sk.voice.state.capturing": "सुन रहा है",
  "sk.voice.state.processing": "प्रोसेस हो रहा है",
  "sk.voice.state.speaking": "बोल रहा है",
  "sk.voice.state.cancelled": "रद्द किया गया",
  "sk.voice.state.denied": "उपलब्ध नहीं",
  "sk.voice.agent.connecting": "कनेक्ट हो रहा है",
  "sk.voice.agent.listening": "सुन रहा है",
  "sk.voice.agent.speaking": "बोल रहा है",
  "sk.voice.agent.interrupt": "बीच में रोकने के लिए टैप करें",
  "sk.voice.agent.interrupted": "बीच में रोका गया",
  "sk.voice.agent.you": "आप",
  "sk.voice.agent.assistant": "असिस्टेंट",
  "sk.voice.agent.live": "लाइव",
  "sk.voice.agent.jumpToLive": "लाइव पर जाएँ",
  "sk.voice.agent.ended": "वॉइस सत्र समाप्त हो गया है।",
  "sk.voice.agent.reconnect": "फिर से कनेक्ट करें",
  "sk.voice.agent.exit": "वॉइस मोड से बाहर निकलें",
  "sk.voice.denied.retry": "फिर से आज़माएँ",
  "sk.voice.consent.title": "kombify होस्टेड प्रोसेसिंग के साथ वॉइस का उपयोग करें?",
  "sk.voice.consent.capture": "क्या कैप्चर होता है: आपके माइक्रोफ़ोन का ऑडियो, केवल रिकॉर्डिंग के दौरान।",
  "sk.voice.consent.destination":
    "यह कहाँ जाता है: आपकी आवाज़ को ट्रांसक्राइब और प्रोसेस करने के लिए api.kombify.io पर kombify होस्टेड वॉइस को।",
  "sk.voice.consent.accept": "स्वीकार करें और वॉइस चालू करें",
  "sk.voice.consent.decline": "अस्वीकार करें",
  "sk.voice.consent.declined": "वॉइस इनपुट बंद है: आपने इस सतह पर वॉइस कैप्चर अस्वीकार किया है।",
  "sk.voice.consent.continuous":
    "वॉइस वार्तालाप पूरे सत्र के दौरान आपके माइक्रोफ़ोन का ऑडियो लगातार kombify होस्टेड वॉइस (api.kombify.io) को स्ट्रीम करता है — केवल रिकॉर्डिंग के दौरान नहीं। वॉइस मोड से बाहर निकलते ही स्ट्रीमिंग रुक जाती है।",
  "sk.voice.state.requesting": "माइक्रोफ़ोन की प्रतीक्षा",
  "sk.voice.notice.mic_denied": "माइक्रोफ़ोन ब्लॉक है",
  "sk.voice.notice.mic_denied.hint": "ब्राउज़र के एड्रेस बार या सेटिंग्स में इस साइट के लिए माइक्रोफ़ोन एक्सेस की अनुमति दें, फिर से आज़माएँ।",
  "sk.voice.notice.mic_unavailable": "कोई माइक्रोफ़ोन नहीं मिला",
  "sk.voice.notice.mic_unavailable.hint": "माइक्रोफ़ोन कनेक्ट करें या कोई दूसरा इनपुट डिवाइस चुनें, फिर से आज़माएँ।",
  "sk.voice.notice.mic_busy": "माइक्रोफ़ोन उपयोग में है",
  "sk.voice.notice.mic_busy.hint": "माइक्रोफ़ोन का उपयोग करने वाला दूसरा ऐप बंद करें, फिर से आज़माएँ।",
  "sk.voice.notice.mic_unsupported": "यहाँ वॉइस इनपुट समर्थित नहीं है",
  "sk.voice.notice.mic_unsupported.hint": "यह ब्राउज़र या पेज ऑडियो कैप्चर नहीं कर सकता। सुरक्षित (HTTPS) पेज पर नया ब्राउज़र उपयोग करें।",
  "sk.voice.notice.connection": "वॉइस से कनेक्ट नहीं हो सका",
  "sk.voice.notice.connection.hint": "अपना कनेक्शन जाँचें और फिर से आज़माएँ।",
  "sk.voice.notice.not_available": "यहाँ वॉइस उपलब्ध नहीं है",
  "sk.voice.notice.not_available.hint": "यह वॉइस सुविधा आपके खाते या वर्कस्पेस के लिए चालू नहीं है।",
  "sk.voice.notice.quota": "वॉइस सीमा पूरी हो गई",
  "sk.voice.notice.quota.hint": "वॉइस की उपयोग सीमा पूरी हो गई है। बाद में फिर से आज़माएँ।",
  "sk.voice.notice.consent": "वॉइस के लिए आपकी पुष्टि ज़रूरी है",
  "sk.voice.notice.consent.hint": "जारी रखने के लिए एक बार वॉइस कैप्चर की पुष्टि करें; आप इसे कभी भी वापस ले सकते हैं।",
  "sk.voice.notice.generic": "वॉइस अचानक रुक गया",
  "sk.voice.notice.generic.hint": "फिर से आज़माएँ। अगर ऐसा होता रहे, तो नीचे दिया गया संदर्भ सपोर्ट के साथ साझा करें।",
  "sk.voice.notice.details": "विवरण",
  "sk.voice.notice.hide_details": "विवरण छिपाएँ",
  "sk.voice.notice.dismiss": "खारिज करें",
  "sk.voice.notice.code": "कोड",
  "sk.voice.notice.reference": "संदर्भ",
  "sk.voice.dialog.label": "वॉइस वार्तालाप",
  "sk.voice.dialog.end": "वार्तालाप समाप्त करें",
  "sk.voice.dialog.hide": "वार्तालाप छिपाएँ",
  "sk.voice.transcript.label": "लाइव ट्रांसक्रिप्ट"
};

export const ar: VoiceUiMessageCatalog = {
  "sk.voice.button.dictation.start": "بدء الإملاء الصوتي",
  "sk.voice.button.dictation.stop": "إيقاف التسجيل",
  "sk.voice.button.cancel": "إلغاء الإدخال الصوتي",
  "sk.voice.button.agent": "التبديل إلى المحادثة الصوتية",
  "sk.voice.button.agent_locked": "المحادثة الصوتية (مقفلة)",
  "sk.voice.state.idle": "جاهز",
  "sk.voice.state.capturing": "يستمع",
  "sk.voice.state.processing": "قيد المعالجة",
  "sk.voice.state.speaking": "يتحدث",
  "sk.voice.state.cancelled": "أُلغي",
  "sk.voice.state.denied": "غير متاح",
  "sk.voice.agent.connecting": "جارٍ الاتصال",
  "sk.voice.agent.listening": "يستمع",
  "sk.voice.agent.speaking": "يتحدث",
  "sk.voice.agent.interrupt": "انقر للمقاطعة",
  "sk.voice.agent.interrupted": "تمت المقاطعة",
  "sk.voice.agent.you": "أنت",
  "sk.voice.agent.assistant": "المساعد",
  "sk.voice.agent.live": "مباشر",
  "sk.voice.agent.jumpToLive": "الانتقال إلى البث المباشر",
  "sk.voice.agent.ended": "انتهت الجلسة الصوتية.",
  "sk.voice.agent.reconnect": "إعادة الاتصال",
  "sk.voice.agent.exit": "الخروج من الوضع الصوتي",
  "sk.voice.denied.retry": "حاول مرة أخرى",
  "sk.voice.consent.title": "هل تريد استخدام الصوت مع المعالجة المستضافة من kombify؟",
  "sk.voice.consent.capture": "ما يتم التقاطه: صوت الميكروفون لديك أثناء التسجيل فقط.",
  "sk.voice.consent.destination":
    "إلى أين يذهب: إلى خدمة الصوت المستضافة من kombify على api.kombify.io لتحويل كلامك إلى نص ومعالجته.",
  "sk.voice.consent.accept": "الموافقة وتفعيل الصوت",
  "sk.voice.consent.decline": "رفض",
  "sk.voice.consent.declined": "الإدخال الصوتي متوقف: لقد رفضت التقاط الصوت على هذه الواجهة.",
  "sk.voice.consent.continuous":
    "تبثّ المحادثة الصوتية صوت الميكروفون لديك باستمرار إلى خدمة الصوت المستضافة من kombify ‏(api.kombify.io) طوال الجلسة كاملة — وليس أثناء التسجيل فقط. يتوقف البث عند خروجك من الوضع الصوتي.",
  "sk.voice.state.requesting": "في انتظار الميكروفون",
  "sk.voice.notice.mic_denied": "الميكروفون محظور",
  "sk.voice.notice.mic_denied.hint": "اسمح بالوصول إلى الميكروفون لهذا الموقع من شريط العنوان أو إعدادات المتصفح، ثم حاول مرة أخرى.",
  "sk.voice.notice.mic_unavailable": "لم يتم العثور على ميكروفون",
  "sk.voice.notice.mic_unavailable.hint": "وصّل ميكروفونًا أو اختر جهاز إدخال آخر، ثم حاول مرة أخرى.",
  "sk.voice.notice.mic_busy": "الميكروفون قيد الاستخدام",
  "sk.voice.notice.mic_busy.hint": "أغلق التطبيق الآخر الذي يستخدم الميكروفون، ثم حاول مرة أخرى.",
  "sk.voice.notice.mic_unsupported": "الإدخال الصوتي غير مدعوم هنا",
  "sk.voice.notice.mic_unsupported.hint": "لا يستطيع هذا المتصفح أو هذه الصفحة التقاط الصوت. استخدم متصفحًا حديثًا على صفحة آمنة (HTTPS).",
  "sk.voice.notice.connection": "تعذّر الاتصال بخدمة الصوت",
  "sk.voice.notice.connection.hint": "تحقق من اتصالك وحاول مرة أخرى.",
  "sk.voice.notice.not_available": "الصوت غير متاح هنا",
  "sk.voice.notice.not_available.hint": "ميزة الصوت هذه غير مفعّلة لحسابك أو لمساحة عملك.",
  "sk.voice.notice.quota": "تم بلوغ حد استخدام الصوت",
  "sk.voice.notice.quota.hint": "تم بلوغ حد استخدام الصوت. حاول مرة أخرى لاحقًا.",
  "sk.voice.notice.consent": "يحتاج الصوت إلى تأكيدك",
  "sk.voice.notice.consent.hint": "أكّد التقاط الصوت مرة واحدة للمتابعة؛ يمكنك إلغاء ذلك في أي وقت.",
  "sk.voice.notice.generic": "توقف الصوت بشكل غير متوقع",
  "sk.voice.notice.generic.hint": "حاول مرة أخرى. إذا تكرر ذلك، شارك المرجع أدناه مع الدعم.",
  "sk.voice.notice.details": "التفاصيل",
  "sk.voice.notice.hide_details": "إخفاء التفاصيل",
  "sk.voice.notice.dismiss": "إغلاق",
  "sk.voice.notice.code": "الرمز",
  "sk.voice.notice.reference": "المرجع",
  "sk.voice.dialog.label": "محادثة صوتية",
  "sk.voice.dialog.end": "إنهاء المحادثة",
  "sk.voice.dialog.hide": "إخفاء المحادثة",
  "sk.voice.transcript.label": "النص المباشر"
};

export const VOICE_UI_CATALOGS: Record<VoiceUiLocale, VoiceUiMessageCatalog> = {
  en,
  de,
  es,
  "zh-Hans": zhHans,
  hi,
  ar
};
