import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import { DashboardApp } from "@/components/dashboard-app";
import type { LogEntry, QuickNote, TranscriptionRecord } from "@/lib/speechkit";

function createMockStorage(): Storage {
  const store = new Map<string, string>();
  return {
    get length() {
      return store.size;
    },
    clear: () => store.clear(),
    getItem: (key: string) => store.get(key) ?? null,
    key: (index: number) => Array.from(store.keys())[index] ?? null,
    removeItem: (key: string) => {
      store.delete(key);
    },
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
  };
}

const {
  windowMinimiseMock,
  windowMaximiseMock,
  windowRestoreMock,
  windowCloseMock,
  windowHideMock,
  windowIsMaximisedMock,
  fetchHistoryMock,
  fetchQuickNotesMock,
  fetchLogsMock,
  fetchDashboardStatsMock,
  fetchDownloadCatalogMock,
  fetchDownloadJobsMock,
  startModelDownloadMock,
  cancelModelDownloadMock,
  selectDownloadedModelMock,
  revealDashboardAudioMock,
  openQuickNoteCaptureMock,
  openQuickNoteEditorMock,
} = vi.hoisted(() => ({
  windowMinimiseMock: vi.fn<() => Promise<void>>(),
  windowMaximiseMock: vi.fn<() => Promise<void>>(),
  windowRestoreMock: vi.fn<() => Promise<void>>(),
  windowCloseMock: vi.fn<() => Promise<void>>(),
  windowHideMock: vi.fn<() => Promise<void>>(),
  windowIsMaximisedMock: vi.fn<() => Promise<boolean>>(),
  fetchHistoryMock: vi.fn<() => Promise<TranscriptionRecord[]>>(),
  fetchQuickNotesMock: vi.fn<() => Promise<QuickNote[]>>(),
  fetchLogsMock: vi.fn<() => Promise<LogEntry[]>>(),
  fetchDashboardStatsMock:
    vi.fn<() => Promise<import("@/lib/speechkit").DashboardStats>>(),
  fetchDownloadCatalogMock: vi.fn(),
  fetchDownloadJobsMock: vi.fn(),
  startModelDownloadMock: vi.fn(),
  cancelModelDownloadMock: vi.fn(),
  selectDownloadedModelMock: vi.fn(),
  revealDashboardAudioMock: vi.fn<() => Promise<string>>(),
  openQuickNoteCaptureMock: vi.fn<() => Promise<string>>(),
  openQuickNoteEditorMock: vi.fn<(noteId?: number) => Promise<string>>(),
}));

vi.mock("@/lib/speechkit", async () => {
  const actual =
    await vi.importActual<typeof import("@/lib/speechkit")>("@/lib/speechkit");
  return {
    ...actual,
    fetchHistory: fetchHistoryMock,
    fetchQuickNotes: fetchQuickNotesMock,
    fetchLogs: fetchLogsMock,
    fetchDashboardStats: fetchDashboardStatsMock,
    fetchDownloadCatalog: fetchDownloadCatalogMock,
    fetchDownloadJobs: fetchDownloadJobsMock,
    startModelDownload: startModelDownloadMock,
    cancelModelDownload: cancelModelDownloadMock,
    selectDownloadedModel: selectDownloadedModelMock,
    revealDashboardAudio: revealDashboardAudioMock,
    openQuickNoteCapture: openQuickNoteCaptureMock,
    openQuickNoteEditor: openQuickNoteEditorMock,
    dashboardAudioDownloadURL: (
      kind: "transcription" | "quicknote",
      id: number,
    ) => `/dashboard/audio?kind=${kind}&id=${id}`,
  };
});

vi.mock("@wailsio/runtime", () => ({
  Window: {
    Minimise: windowMinimiseMock,
    Maximise: windowMaximiseMock,
    Restore: windowRestoreMock,
    Close: windowCloseMock,
    Hide: windowHideMock,
    IsMaximised: windowIsMaximisedMock,
  },
}));

describe("DashboardApp", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn> | undefined;
  let storageMock: Storage;

  beforeEach(() => {
    storageMock = createMockStorage();
    Object.defineProperty(window, "localStorage", {
      value: storageMock,
      configurable: true,
    });
    window.history.replaceState({}, "", "/");
    window.sessionStorage.clear();
    storageMock.clear();

    windowMinimiseMock.mockReset();
    windowMaximiseMock.mockReset();
    windowRestoreMock.mockReset();
    windowCloseMock.mockReset();
    windowHideMock.mockReset();
    windowIsMaximisedMock.mockReset();
    windowMinimiseMock.mockResolvedValue(undefined);
    windowMaximiseMock.mockResolvedValue(undefined);
    windowRestoreMock.mockResolvedValue(undefined);
    windowCloseMock.mockResolvedValue(undefined);
    windowHideMock.mockResolvedValue(undefined);
    windowIsMaximisedMock.mockResolvedValue(false);

    const originalFetch = window.fetch.bind(window);
    fetchSpy = vi
      .spyOn(window, "fetch")
      .mockImplementation(async (input, init) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.toString()
              : (input as Request).url;
        if (url === "/app/setup-status") {
          return new Response(JSON.stringify({ setupDone: true }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url === "/app/version") {
          return new Response(JSON.stringify({ version: "0.18.0" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url === "/app/update/jobs") {
          return new Response(JSON.stringify([]), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url === "/app/update/download") {
          return new Response(
            JSON.stringify({
              id: "update-job-1",
              version: "0.19.0",
              assetName: "SpeechKit-Setup-v0.19.0.exe",
              status: "running",
              progress: 0.42,
              bytesDone: 42,
              totalBytes: 100,
              statusText: "42 / 100 MB",
            }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
          );
        }
        return originalFetch(input, init);
      });

    fetchHistoryMock.mockReset();
    fetchQuickNotesMock.mockReset();
    fetchLogsMock.mockReset();
    fetchDashboardStatsMock.mockReset();
    fetchDownloadCatalogMock.mockReset();
    fetchDownloadJobsMock.mockReset();
    startModelDownloadMock.mockReset();
    cancelModelDownloadMock.mockReset();
    selectDownloadedModelMock.mockReset();
    revealDashboardAudioMock.mockReset();
    openQuickNoteCaptureMock.mockReset();
    openQuickNoteEditorMock.mockReset();
    fetchHistoryMock.mockResolvedValue([]);
    fetchQuickNotesMock.mockResolvedValue([]);
    fetchLogsMock.mockResolvedValue([]);
    fetchDashboardStatsMock.mockResolvedValue({
      transcriptions: 12,
      quickNotes: 3,
      totalWords: 248,
      totalAudioDurationMs: 180000,
      averageWordsPerMinute: 82.7,
      averageLatencyMs: 410,
    });
    fetchDownloadCatalogMock.mockResolvedValue([]);
    fetchDownloadJobsMock.mockResolvedValue([]);
    startModelDownloadMock.mockResolvedValue({
      id: "model-job-1",
      modelId: "whisper.ggml-large-v3-turbo",
      profileId: "stt.local.whispercpp",
      status: "running",
      progress: 0.42,
      bytesDone: 42,
      totalBytes: 100,
      statusText: "42 / 100 MB",
    });
    cancelModelDownloadMock.mockResolvedValue(undefined);
    selectDownloadedModelMock.mockResolvedValue({ message: "Selected" });
    revealDashboardAudioMock.mockResolvedValue("Opened");
    openQuickNoteCaptureMock.mockResolvedValue("Capture opened");
    openQuickNoteEditorMock.mockResolvedValue("Editor opened");
  });

  afterEach(() => {
    fetchSpy?.mockRestore();
    window.history.replaceState({}, "", "/");
    window.sessionStorage.clear();
    storageMock.clear();
  });

  it("opens on a dashboard page with sidebar navigation and KPIs", async () => {
    render(<DashboardApp />);

    expect(
      await screen.findByText(/welcome to speechkit/i),
    ).toBeInTheDocument();
    expect(await screen.findByText("v0.18.0")).toBeInTheDocument();
    expect(screen.getByTestId("welcome-scroll")).toHaveClass("overflow-y-auto");
    expect(screen.getByTestId("welcome-kpis")).toHaveClass("grid");
    expect(screen.getByRole("button", { name: "Library" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Dashboard" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Voice workflow desktop")).not.toBeInTheDocument();
    expect(screen.getByText(/quick start/i)).toBeInTheDocument();
    expect(await screen.findByText("82.7")).toBeInTheDocument();
    expect(screen.getByText(/average wpm/i)).toBeInTheDocument();
    expect(screen.getByText(/hold windows alt to talk/i)).toBeInTheDocument();
    expect(screen.getByText(/hover over the pill/i)).toBeInTheDocument();
    expect(screen.getByText(/say summarize/i)).toBeInTheDocument();
  });

  it("persists the selected desktop theme and wires the custom window controls", async () => {
    render(<DashboardApp />);

    const themeToggle = await screen.findByRole("button", {
      name: /switch to light mode/i,
    });
    expect(document.documentElement.classList.contains("dark")).toBe(true);

    fireEvent.click(themeToggle);

    await waitFor(() => {
      expect(document.documentElement.dataset.theme).toBe("light");
      expect(document.documentElement.classList.contains("dark")).toBe(false);
      expect(storageMock.getItem("speechkit.desktop.theme")).toBe("light");
    });

    fireEvent.click(screen.getByRole("button", { name: /minimise window/i }));
    fireEvent.click(screen.getByRole("button", { name: /maximise window/i }));
    fireEvent.click(screen.getByRole("button", { name: /close window/i }));

    await waitFor(() => expect(windowMinimiseMock).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(windowMaximiseMock).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(windowHideMock).toHaveBeenCalledTimes(1));
    expect(windowCloseMock).not.toHaveBeenCalled();
  });

  it("replaces onboarding with an activity home once transcriptions exist", async () => {
    fetchHistoryMock.mockResolvedValue([
      {
        id: 2,
        text: "newer transcription",
        language: "de",
        provider: "local",
        model: "whisper.cpp",
        latencyMs: 110,
        createdAt: "2026-03-26T09:30:00",
      },
    ]);
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 11,
        text: "Call with AcmeOS team",
        language: "de",
        provider: "manual",
        latencyMs: 0,
        pinned: true,
        createdAt: "2026-03-26T09:30:00",
        updatedAt: "2026-03-26T09:30:00",
      },
    ]);

    render(<DashboardApp />);

    expect(await screen.findByText(/recent activity/i)).toBeInTheDocument();
    expect(screen.getByText(/latest transcription/i)).toBeInTheDocument();
    expect(screen.getByText(/newer transcription/i)).toBeInTheDocument();
    expect(screen.getByText(/pinned notes/i)).toBeInTheDocument();
    expect(screen.getByText(/call with acmeos team/i)).toBeInTheDocument();
    expect(screen.queryByText(/quick start/i)).not.toBeInTheDocument();
    expect(
      screen.queryByText(/hold windows alt to talk/i),
    ).not.toBeInTheDocument();
  });

  it("shows library entries sorted by newest date and includes absolute timestamps", async () => {
    fetchHistoryMock.mockResolvedValue([
      {
        id: 1,
        text: "older transcription",
        language: "de",
        provider: "hf",
        latencyMs: 120,
        createdAt: "2026-03-24T08:15:00",
      },
      {
        id: 2,
        text: "newer transcription",
        language: "de",
        provider: "hf",
        latencyMs: 110,
        createdAt: "2026-03-26T09:30:00",
      },
    ]);
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 10,
        text: "older note",
        language: "de",
        provider: "manual",
        latencyMs: 0,
        pinned: false,
        createdAt: "2026-03-24T07:45:00",
        updatedAt: "2026-03-24T07:45:00",
      },
      {
        id: 11,
        text: "newer note",
        language: "de",
        provider: "manual",
        latencyMs: 0,
        pinned: true,
        createdAt: "2026-03-26T09:30:00",
        updatedAt: "2026-03-26T09:30:00",
      },
    ]);

    render(<DashboardApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Library" }));

    await waitFor(() => expect(fetchHistoryMock).toHaveBeenCalled());

    const transcriptionRows = await screen.findAllByTestId("transcription-row");
    expect(transcriptionRows[0]).toHaveTextContent("newer transcription");
    expect(transcriptionRows[1]).toHaveTextContent("older transcription");

    expect(screen.getByText(/pinned notes/i)).toBeInTheDocument();
    const quickNoteRows = await screen.findAllByTestId("quicknote-row");
    expect(quickNoteRows[0]).toHaveTextContent("newer note");
    expect(quickNoteRows[1]).toHaveTextContent("older note");
    expect(quickNoteRows[0]).toHaveTextContent(
      /26\/03\/2026.*09:30|09:30.*26\/03\/2026/,
    );
    expect(quickNoteRows[0]).toHaveTextContent(/pinned/i);
    expect(screen.queryByText(/show all notes/i)).not.toBeInTheDocument();
  });

  it("shows audio actions for records with stored audio", async () => {
    fetchHistoryMock.mockResolvedValue([
      {
        id: 2,
        text: "newer transcription",
        language: "de",
        provider: "hf",
        model: "openai/whisper-large-v3",
        durationMs: 2400,
        latencyMs: 110,
        audio: {
          storageKind: "local-file",
          mimeType: "audio/wav",
          sizeBytes: 4096,
          durationMs: 2400,
        },
        createdAt: "2026-03-26T09:30:00",
      } as unknown as TranscriptionRecord,
    ]);
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 11,
        text: "newer note",
        language: "de",
        provider: "capture",
        durationMs: 1800,
        latencyMs: 0,
        audio: {
          storageKind: "local-file",
          mimeType: "audio/wav",
          sizeBytes: 2048,
          durationMs: 1800,
        },
        pinned: true,
        createdAt: "2026-03-26T09:30:00",
        updatedAt: "2026-03-26T09:30:00",
      },
    ]);

    render(<DashboardApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Library" }));

    expect(await screen.findByText("2.4s")).toBeInTheDocument();
    expect(screen.getByText("1.8s")).toBeInTheDocument();
    expect(screen.queryByText(/wav 4 kb/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/wav 2 kb/i)).not.toBeInTheDocument();
    expect(screen.getByText("large-v3")).toBeInTheDocument();

    fireEvent.click(screen.getAllByRole("button", { name: /show file/i })[0]);

    await waitFor(() =>
      expect(revealDashboardAudioMock).toHaveBeenCalledWith("transcription", 2),
    );

    const downloadLinks = screen.getAllByRole("link", {
      name: /download audio/i,
    });
    expect(downloadLinks[0]).toHaveAttribute(
      "href",
      "/dashboard/audio?kind=transcription&id=2",
    );
    expect(downloadLinks[1]).toHaveAttribute(
      "href",
      "/dashboard/audio?kind=quicknote&id=11",
    );
  });

  it("opens the requested tab from the location hash and persists the selection", async () => {
    window.history.replaceState({}, "", "/dashboard.html#logs");

    render(<DashboardApp />);

    expect(await screen.findByText(/application logs/i)).toBeInTheDocument();
    await waitFor(() => expect(fetchLogsMock).toHaveBeenCalled());
    expect(window.sessionStorage.getItem("speechkit.dashboard.tab")).toBe(
      "logs",
    );
  });

  it("restores the last selected tab from session storage when no hash is set", async () => {
    window.sessionStorage.setItem("speechkit.dashboard.tab", "library");
    fetchHistoryMock.mockResolvedValue([
      {
        id: 2,
        text: "restored transcription",
        language: "de",
        provider: "hf",
        latencyMs: 110,
        createdAt: "2026-03-26T09:30:00",
      },
    ]);

    render(<DashboardApp />);

    expect(
      await screen.findByRole("heading", { name: "Library" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("restored transcription"),
    ).toBeInTheDocument();
  });

  it("opens quick capture from the dashboard quick note action through the client wrapper", async () => {
    render(<DashboardApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Quick Note" }));

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith("/quicknotes/open-capture", {
        method: "POST",
      }),
    );
  });

  it("opens quick note editor actions through the client wrapper", async () => {
    fetchQuickNotesMock.mockResolvedValue([
      {
        id: 11,
        text: "newer note",
        language: "de",
        provider: "manual",
        latencyMs: 0,
        pinned: true,
        createdAt: "2026-03-26T09:30:00",
        updatedAt: "2026-03-26T09:30:00",
      },
    ]);

    render(<DashboardApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Library" }));
    fireEvent.click(await screen.findByRole("button", { name: "+ New" }));

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith("/quicknotes/open-editor", {
        method: "POST",
      }),
    );

    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith("/quicknotes/open-editor?id=11", {
        method: "POST",
      }),
    );
  });

  it("shows an in-app update banner with change log link and download progress", async () => {
    fetchSpy?.mockImplementation(async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.toString()
            : (input as Request).url;
      if (url === "/app/setup-status") {
        return new Response(JSON.stringify({ setupDone: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/version") {
        return new Response(
          JSON.stringify({
            version: "0.18.0",
            latestVersion: "0.19.0",
            updateURL: "https://example.com/releases/tag/v0.19.0",
            downloadURL:
              "https://example.com/releases/download/v0.19.0/SpeechKit-Setup-v0.19.0.exe",
            downloadSizeBytes: 100,
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === "/app/update/jobs") {
        return new Response(JSON.stringify([]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/update/download") {
        return new Response(
          JSON.stringify({
            id: "update-job-1",
            version: "0.19.0",
            assetName: "SpeechKit-Setup-v0.19.0.exe",
            status: "running",
            progress: 0.42,
            bytesDone: 42,
            totalBytes: 100,
            statusText: "42 / 100 MB",
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      return new Response("{}", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    render(<DashboardApp />);

    expect(
      await screen.findByText(/update available: v0.19.0/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /change log/i })).toHaveAttribute(
      "href",
      "https://example.com/releases/tag/v0.19.0",
    );

    fireEvent.click(screen.getByRole("button", { name: "Download" }));

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith(
        "/app/update/download",
        expect.objectContaining({ method: "POST" }),
      ),
    );
    expect(await screen.findByText("42 / 100 MB")).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /cancel download/i })[0],
    ).toBeInTheDocument();
  });

  it("offers local small and turbo downloads during onboarding and keeps progress visible", async () => {
    fetchSpy?.mockImplementation(async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.toString()
            : (input as Request).url;
      if (url === "/app/setup-status") {
        return new Response(JSON.stringify({ setupDone: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/version") {
        return new Response(JSON.stringify({ version: "0.18.0" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/complete-setup") {
        return new Response(JSON.stringify({ setupDone: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/settings/update") {
        return new Response(JSON.stringify({ message: "Saved" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("{}", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    fetchDownloadCatalogMock.mockResolvedValue([
      {
        id: "whisper.ggml-small",
        profileId: "stt.local.whispercpp",
        name: "Whisper Small Multilingual (466 MB)",
        description: "Small local model",
        sizeLabel: "466 MB",
        sizeBytes: 484264096,
        kind: "http",
        license: "mit",
        available: false,
        selected: false,
      },
      {
        id: "whisper.ggml-large-v3-turbo",
        profileId: "stt.local.whispercpp",
        name: "Whisper Large v3 Turbo",
        description: "Turbo local model",
        sizeLabel: "~1.6 GB",
        sizeBytes: 1624555275,
        kind: "http",
        license: "mit",
        available: false,
        recommended: true,
        selected: false,
      },
    ]);

    render(<DashboardApp />);

    fireEvent.click(
      await screen.findByRole("button", { name: /get started/i }),
    );

    expect(
      await screen.findByText("Whisper Large v3 Turbo"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Whisper Small Multilingual (466 MB)"),
    ).toBeInTheDocument();
    expect(screen.getByText("Recommended")).toBeInTheDocument();

    const continueButton = screen.getByRole("button", { name: /^continue$/i });
    expect(continueButton).toBeEnabled();

    fireEvent.click(
      screen.getByRole("button", { name: "Download Whisper Large v3 Turbo" }),
    );

    await waitFor(() =>
      expect(startModelDownloadMock).toHaveBeenCalledWith(
        "whisper.ggml-large-v3-turbo",
      ),
    );
    expect((await screen.findAllByText("42 / 100 MB")).length).toBeGreaterThan(
      0,
    );
    expect(
      screen.getAllByRole("button", { name: /cancel download/i }).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("Chosen for setup")).toBeInTheDocument();
  });

  it("shows and saves two-key defaults on the onboarding completion screen", async () => {
    fetchSpy?.mockImplementation(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === "string"
            ? input
            : input instanceof URL
              ? input.toString()
              : (input as Request).url;
        if (url === "/app/setup-status") {
          return new Response(JSON.stringify({ setupDone: false }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url === "/app/version") {
          return new Response(JSON.stringify({ version: "0.18.0" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url === "/settings/update") {
          expect(init?.body).toBeInstanceOf(URLSearchParams);
          return new Response(JSON.stringify({ message: "Saved" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url === "/app/complete-setup") {
          return new Response(JSON.stringify({ setupDone: true }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        return new Response("{}", {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      },
    );

    render(<DashboardApp />);

    fireEvent.click(
      await screen.findByRole("button", { name: /get started/i }),
    );
    fireEvent.click(await screen.findByRole("button", { name: /^continue$/i }));

    expect(await screen.findByText("Win+Alt")).toBeInTheDocument();
    expect(screen.getByText("Ctrl+Win")).toBeInTheDocument();
    expect(screen.getByText("Ctrl+Shift")).toBeInTheDocument();
    expect(screen.queryByText("Ctrl+Shift+D")).not.toBeInTheDocument();
    expect(screen.queryByText("Ctrl+Shift+A")).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: /start using speechkit/i }),
    );

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith(
        "/settings/update",
        expect.objectContaining({ method: "POST" }),
      ),
    );
    const fetchCalls = fetchSpy?.mock.calls as
      | Array<[RequestInfo | URL, RequestInit?]>
      | undefined;
    const settingsCall = fetchCalls?.find(
      (call) => call[0] === "/settings/update",
    );
    const body = settingsCall?.[1]?.body as URLSearchParams;
    expect(body.get("dictate_hotkey")).toBe("win+alt");
    expect(body.get("assist_hotkey")).toBe("ctrl+win");
    expect(body.get("voice_agent_hotkey")).toBe("ctrl+shift");
  });

  it("opens transcribe settings when the user wants to use a cloud token instead of a local model", async () => {
    fetchSpy?.mockImplementation(async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.toString()
            : (input as Request).url;
      if (url === "/app/setup-status") {
        return new Response(JSON.stringify({ setupDone: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/version") {
        return new Response(JSON.stringify({ version: "0.18.0" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/complete-setup") {
        return new Response(JSON.stringify({ setupDone: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("{}", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    fetchDownloadCatalogMock.mockResolvedValue([
      {
        id: "whisper.ggml-small",
        profileId: "stt.local.whispercpp",
        name: "Whisper Small Multilingual (466 MB)",
        description: "Small local model",
        sizeLabel: "466 MB",
        sizeBytes: 484264096,
        kind: "http",
        license: "mit",
        available: false,
        selected: false,
      },
      {
        id: "whisper.ggml-large-v3-turbo",
        profileId: "stt.local.whispercpp",
        name: "Whisper Large v3 Turbo",
        description: "Turbo local model",
        sizeLabel: "~1.6 GB",
        sizeBytes: 1624555275,
        kind: "http",
        license: "mit",
        recommended: true,
        available: false,
        selected: false,
      },
    ]);

    render(<DashboardApp />);

    fireEvent.click(
      await screen.findByRole("button", { name: /get started/i }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /use hugging face token instead/i }),
    );

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith("/app/complete-setup", {
        method: "POST",
      }),
    );
    expect(await screen.findByText("Speech-to-Text")).toBeInTheDocument();
  });

  it("lets the user skip setup even before a local model is downloaded", async () => {
    fetchSpy?.mockImplementation(async (input: RequestInfo | URL) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.toString()
            : (input as Request).url;
      if (url === "/app/setup-status") {
        return new Response(JSON.stringify({ setupDone: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/version") {
        return new Response(JSON.stringify({ version: "0.18.0" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/app/complete-setup") {
        return new Response(JSON.stringify({ setupDone: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("{}", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    render(<DashboardApp />);

    fireEvent.click(await screen.findByRole("button", { name: /skip setup/i }));

    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith("/app/complete-setup", {
        method: "POST",
      }),
    );
    expect(
      await screen.findByRole("heading", { name: /dashboard/i }),
    ).toBeInTheDocument();
  });
});
