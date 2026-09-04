import { beforeEach, describe, expect, it } from "vitest";
import { chromeStore, resetUsageStrip, setUsageFromSSE } from "./chrome.js";

describe("chrome usage strip", () => {
	beforeEach(() => {
		resetUsageStrip();
	});

	it("treats top-level usage as a cumulative snapshot", () => {
		setUsageFromSSE({
			prompt_tokens: 100,
			completion_tokens: 20,
			prompt_cache_hit_tokens: 80,
			round_prompt_tokens: 100,
			round_completion_tokens: 20,
		});
		setUsageFromSSE({
			prompt_tokens: 100,
			completion_tokens: 20,
			prompt_cache_hit_tokens: 80,
		});

		expect(chromeStore.usageStrip).toMatchObject({ prompt: 100, completion: 20, hit: 80 });
	});

	it("does not add a later cumulative snapshot to the previous one", () => {
		setUsageFromSSE({ prompt_tokens: 100, completion_tokens: 20, round_prompt_tokens: 100, round_completion_tokens: 20 });
		setUsageFromSSE({ prompt_tokens: 150, completion_tokens: 30, round_prompt_tokens: 50, round_completion_tokens: 10 });

		expect(chromeStore.usageStrip).toMatchObject({ prompt: 150, completion: 30 });
	});

});
