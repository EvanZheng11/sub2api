#!/usr/bin/env python3
"""Record the routing golden fixtures from pinned source, never the checkout.

main: 从 routingMatrixMainRevision 固定的上游 main 修订录制整份矩阵基准。
--opencode-go-fork: 从 routingMatrixOpenCodeGoForkRevision 录制的 fork 基准，
只包含 opencode_go 切片（该平台的模型前缀归一、凭据 base URL 优先、本地化凭据
错误文案是本地适配，与上游 main 必然不同）。
"""

import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile

MATRIX_DRIVER = "upstream_protocol_routing_matrix_test.go"

MAIN_RECORDER = r'''

func TestRecordUpstreamProtocolRoutingMainGolden(t *testing.T) {
    gin.SetMode(gin.TestMode)
    data, err := json.MarshalIndent(captureRoutingMatrixGolden(), "", "  ")
    require.NoError(t, err)
    require.NoError(t, os.WriteFile(os.Getenv("ROUTING_MATRIX_RECORD_OUTPUT"), append(data, '\n'), 0o644))
}
'''

FORK_RECORDER = r'''

func TestRecordUpstreamProtocolRoutingOpenCodeGoForkGolden(t *testing.T) {
    gin.SetMode(gin.TestMode)
    data, err := json.MarshalIndent(captureRoutingMatrixGoldenFor(routingMatrixCasesForPlatform(PlatformOpenCodeGo), routingMatrixOpenCodeGoForkRevision), "", "  ")
    require.NoError(t, err)
    require.NoError(t, os.WriteFile(os.Getenv("ROUTING_MATRIX_RECORD_OUTPUT"), append(data, '\n'), 0o644))
}
'''

RECORDINGS = {
    "main": {
        "revision_const": "routingMatrixMainRevision",
        "recorder": MAIN_RECORDER,
        "recorder_test": "TestRecordUpstreamProtocolRoutingMainGolden",
        "golden_path": "testdata/upstream_protocol_routing_main_golden.json",
    },
    "opencode-go-fork": {
        "revision_const": "routingMatrixOpenCodeGoForkRevision",
        "recorder": FORK_RECORDER,
        "recorder_test": "TestRecordUpstreamProtocolRoutingOpenCodeGoForkGolden",
        "golden_path": "testdata/upstream_protocol_routing_opencode_go_fork_golden.json",
    },
}


def format_golden(fixture):
    """Keep observations readable and wrap case indexes in compact rows."""
    metadata = {key: value for key, value in fixture.items() if key != "results"}
    prefix = json.dumps(metadata, ensure_ascii=False, indent=2)[:-2]
    indexes = fixture["results"]
    rows = ["    " + ", ".join(map(str, indexes[i:i + 32])) for i in range(0, len(indexes), 32)]
    return (prefix + ',\n  "results": [\n' + ",\n".join(rows) + "\n  ]\n}\n").encode("utf-8")


def record(repo, driver, source, recording):
    """Export the pinned revision, inject the current driver plus the recorder, run it."""
    revision = re.search(r'const %s = "([0-9a-f]{40})"' % recording["revision_const"], source).group(1)
    subprocess.run(["git", "cat-file", "-e", revision + "^{commit}"], cwd=repo, check=True)
    archive = subprocess.check_output(["git", "archive", revision, "backend"], cwd=repo)

    with tempfile.TemporaryDirectory(prefix="sub2api-routing-main-") as scratch:
        root = Path(scratch)
        with tarfile.open(fileobj=io.BytesIO(archive)) as files:
            files.extractall(root, filter="data")
        recording_driver = root / "backend/internal/service" / MATRIX_DRIVER
        recording_driver.write_text(source + recording["recorder"])
        output = root / "routing_golden.json"
        environment = dict(os.environ, ROUTING_MATRIX_RECORD_OUTPUT=str(output))
        subprocess.run(
            ["go", "test", "-tags", "unit", "./internal/service/", "-run", "^%s$" % recording["recorder_test"], "-count=1"],
            cwd=root / "backend", env=environment, check=True,
        )
        fixture = json.loads(output.read_bytes())
        if fixture["source_revision"] != revision:
            raise RuntimeError("Recorded source revision differs from the pinned revision")
        destination = driver.parent / recording["golden_path"]
        destination.write_bytes(format_golden(fixture))
        print(f"Recorded {fixture['case_count']} cases from {revision} into {destination}")


def main():
    repo = Path(__file__).resolve().parents[4]
    driver = repo / "backend/internal/service" / MATRIX_DRIVER
    mode = "opencode-go-fork" if "--opencode-go-fork" in sys.argv[1:] else "main"
    record(repo, driver, driver.read_text(), RECORDINGS[mode])


if __name__ == "__main__":
    main()
