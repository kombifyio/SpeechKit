import {
  fetchSettingsStateMock,
  saveSettingsStateMock,
  openFileDialogMock,
  openStorageSettings,
} from "@/components/settings-test-harness";
import {
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { baseSettings } from "@/components/settings-test-fixtures";
import { SettingsApp } from "@/components/settings-app";

const postgresTestDSN = [
  "postgres://",
  "speechkit:secret@localhost:5432/speechkit?sslmode=disable",
].join("");

describe("SettingsApp server and storage", () => {
  it("saves local audio storage preferences", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);
    await openStorageSettings();

    const saveAudioToggle = await screen.findByRole("switch", {
      name: "Save raw audio locally",
    });
    fireEvent.click(saveAudioToggle);

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ saveAudio: false }),
      ),
    );

    const retentionSelect = await screen.findByLabelText("Audio retention");
    fireEvent.change(retentionSelect, { target: { value: "30" } });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ audioRetentionDays: 30 }),
      ),
    );
  });

  it("saves the default model download directory from storage settings", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      modelDownloadDir: "C:/Users/testuser/AppData/Local/SpeechKit/models",
    });

    render(<SettingsApp />);
    await openStorageSettings();

    const input = await screen.findByLabelText("Default model download folder");
    fireEvent.change(input, {
      target: { value: "D:/AI/SpeechKitModels" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ modelDownloadDir: "D:/AI/SpeechKitModels" }),
      ),
    );
  });

  it("expands the storage card across the settings content width", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);
    await openStorageSettings();

    const storageCard = await screen.findByTestId("storage-settings-card");
    expect(storageCard).toHaveClass("xl:col-span-2");
    expect(storageCard).not.toHaveClass("sk-panel");
    expect(storageCard).not.toHaveClass("rounded-[24px]");
  });

  it("opens a native folder picker for the SQLite storage folder", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      sqlitePath: "C:/Users/testuser/AppData/Roaming/SpeechKit/feedback.db",
    });
    openFileDialogMock.mockResolvedValue("D:\\SpeechKitData");

    render(<SettingsApp />);
    await openStorageSettings();

    fireEvent.click(
      await screen.findByRole("button", {
        name: "Choose SQLite storage folder",
      }),
    );

    await waitFor(() =>
      expect(openFileDialogMock).toHaveBeenCalledWith(
        expect.objectContaining({
          CanChooseDirectories: true,
          CanChooseFiles: false,
          CanCreateDirectories: true,
          Directory: "C:/Users/testuser/AppData/Roaming/SpeechKit",
        }),
      ),
    );
    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          sqlitePath: "D:\\SpeechKitData\\feedback.db",
        }),
      ),
    );
  });

  it("opens a native folder picker for model downloads", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      modelDownloadDir: "C:/Users/testuser/AppData/Local/SpeechKit/models",
    });
    openFileDialogMock.mockResolvedValue("E:\\Models\\SpeechKit");

    render(<SettingsApp />);
    await openStorageSettings();

    fireEvent.click(
      await screen.findByRole("button", {
        name: "Choose model download folder",
      }),
    );

    await waitFor(() =>
      expect(openFileDialogMock).toHaveBeenCalledWith(
        expect.objectContaining({
          CanChooseDirectories: true,
          CanChooseFiles: false,
          CanCreateDirectories: true,
          Directory: "C:/Users/testuser/AppData/Local/SpeechKit/models",
        }),
      ),
    );
    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          modelDownloadDir: "E:\\Models\\SpeechKit",
        }),
      ),
    );
  });

  it("persists the postgres connection string once the backend is configured locally", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);
    await openStorageSettings();

    fireEvent.click(await screen.findByRole("button", { name: "PostgreSQL" }));

    fireEvent.change(
      await screen.findByLabelText("PostgreSQL connection string"),
      {
        target: { value: postgresTestDSN },
      },
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          storeBackend: "postgres",
          postgresDSN: postgresTestDSN,
        }),
      ),
    );
  });

  it("shows backend-specific storage copy and validation hints", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);
    await openStorageSettings();

    expect(await screen.findByLabelText("SQLite path")).toBeInTheDocument();
    expect(
      screen.queryByLabelText("PostgreSQL connection string"),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "PostgreSQL" }));

    expect(
      await screen.findByLabelText("PostgreSQL connection string"),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("SQLite path")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        /add a postgresql connection string before switching the metadata backend/i,
      ),
    ).toBeInTheDocument();
  });

  it("does not persist the postgres backend switch until a connection string is present", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);
    await openStorageSettings();

    fireEvent.click(await screen.findByRole("button", { name: "PostgreSQL" }));

    await screen.findByLabelText("PostgreSQL connection string");
    expect(saveSettingsStateMock).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("PostgreSQL connection string"), {
      target: { value: postgresTestDSN },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          storeBackend: "postgres",
          postgresDSN: postgresTestDSN,
        }),
      ),
    );
  });

  it("persists the local audio storage cap", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);
    await openStorageSettings();

    const input = await screen.findByLabelText("Max local audio storage (MB)");
    fireEvent.change(input, {
      target: { value: "1024" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ maxAudioStorageMB: 1024 }),
      ),
    );
  });
});
