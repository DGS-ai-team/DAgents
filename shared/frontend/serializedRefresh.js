/**
 * Serialize snapshot refreshes and discard results superseded while a newer
 * refresh was requested. This keeps slow responses from briefly regressing a
 * live UI to an older snapshot.
 *
 * @template T
 * @param {() => Promise<T>} fetchSnapshot
 * @param {(snapshot: T) => void} applySnapshot
 */
export function createSerializedRefresh(fetchSnapshot, applySnapshot) {
  let generation = 0;
  let inFlight = null;

  function refresh() {
    const requestedGeneration = generation;
    if (inFlight && inFlight.generation === requestedGeneration) {
      inFlight.queued = true;
      return inFlight.promise;
    }

    const state = {
      generation: requestedGeneration,
      queued: false,
      promise: null,
    };

    state.promise = (async () => {
      do {
        state.queued = false;
        let snapshot;
        try {
          snapshot = await fetchSnapshot();
        } catch {
          // Polling callers intentionally ignore transient request failures.
          // A queued refresh must still get a chance to run.
          continue;
        }

        if (state.generation !== generation) break;

        // A newer request arrived while this response was in flight. Do not
        // apply the intermediate result: it could visibly roll the UI back
        // (for example, from a pending approval to an older empty list).
        if (state.queued) continue;

        applySnapshot(snapshot);
      } while (state.queued && state.generation === generation);
    })().finally(() => {
      if (inFlight === state) inFlight = null;
    });

    inFlight = state;
    return state.promise;
  }

  function reset() {
    generation += 1;
  }

  return { refresh, reset };
}
