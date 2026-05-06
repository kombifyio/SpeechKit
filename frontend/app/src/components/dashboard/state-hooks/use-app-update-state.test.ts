import { describe, expect, it } from "vitest";

import {
  isActiveAppUpdateJob,
  mergeAppUpdateJobs,
  pickLatestAppUpdateJob,
  upsertAppUpdateJob,
} from "@/components/dashboard/state-hooks/use-app-update-state";
import type { AppUpdateJob } from "@/lib/speechkit";

function job(id: string, status: AppUpdateJob["status"]): AppUpdateJob {
  return {
    id,
    version: "0.29.0",
    assetName: `SpeechKit-${id}.exe`,
    status,
    progress: status === "done" ? 1 : 0.2,
    bytesDone: status === "done" ? 100 : 20,
    totalBytes: 100,
    statusText: status,
  };
}

describe("app update state helpers", () => {
  it("keeps active stale jobs and drops completed stale jobs while merging refreshed jobs", () => {
    const merged = mergeAppUpdateJobs(
      [job("active", "running"), job("old", "done"), job("replace", "pending")],
      [job("replace", "done"), job("new", "running")],
    );

    expect(merged.map((item) => `${item.id}:${item.status}`)).toEqual([
      "active:running",
      "replace:done",
      "new:running",
    ]);
  });

  it("upserts by id and reports latest/active jobs", () => {
    const initial = [job("a", "pending")];
    const next = upsertAppUpdateJob(initial, job("a", "done"));
    const appended = upsertAppUpdateJob(next, job("b", "running"));

    expect(next).toHaveLength(1);
    expect(next[0]?.status).toBe("done");
    expect(pickLatestAppUpdateJob(appended)?.id).toBe("b");
    expect(isActiveAppUpdateJob(appended[0]!)).toBe(false);
    expect(isActiveAppUpdateJob(appended[1]!)).toBe(true);
  });
});
