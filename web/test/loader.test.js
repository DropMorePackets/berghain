import assert from "node:assert/strict";
import test from "node:test";

import {formatDuration} from "../src/challange/loader.js";

test("formats ban durations", () => {
    assert.equal(formatDuration(0), "00:00:00");
    assert.equal(formatDuration(65), "00:01:05");
    assert.equal(formatDuration(3661), "01:01:01");
    assert.equal(formatDuration(90061), "1d 01:01:01");
});
