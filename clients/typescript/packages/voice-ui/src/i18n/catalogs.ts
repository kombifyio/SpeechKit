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
  | "sk.voice.consent.continuous";

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
    "Voice conversation streams your microphone continuously to kombify hosted voice (api.kombify.io) for the entire session — not only while you press record. Streaming stops when you exit voice mode."
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
    "Die Sprachkonversation streamt dein Mikrofon während der gesamten Sitzung fortlaufend an kombify Hosted Voice (api.kombify.io) — nicht nur während du aufnimmst. Das Streaming endet, sobald du den Sprachmodus verlässt."
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
    "La conversación por voz transmite tu micrófono de forma continua a la voz alojada de kombify (api.kombify.io) durante toda la sesión, no solo mientras grabas. La transmisión se detiene al salir del modo de voz."
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
    "语音对话会在整个会话期间将你的麦克风音频持续传输到 kombify 托管语音服务（api.kombify.io），而不仅是在录音时。退出语音模式后传输即停止。"
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
    "वॉइस वार्तालाप पूरे सत्र के दौरान आपके माइक्रोफ़ोन का ऑडियो लगातार kombify होस्टेड वॉइस (api.kombify.io) को स्ट्रीम करता है — केवल रिकॉर्डिंग के दौरान नहीं। वॉइस मोड से बाहर निकलते ही स्ट्रीमिंग रुक जाती है।"
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
    "تبثّ المحادثة الصوتية صوت الميكروفون لديك باستمرار إلى خدمة الصوت المستضافة من kombify ‏(api.kombify.io) طوال الجلسة كاملة — وليس أثناء التسجيل فقط. يتوقف البث عند خروجك من الوضع الصوتي."
};

export const VOICE_UI_CATALOGS: Record<VoiceUiLocale, VoiceUiMessageCatalog> = {
  en,
  de,
  es,
  "zh-Hans": zhHans,
  hi,
  ar
};
