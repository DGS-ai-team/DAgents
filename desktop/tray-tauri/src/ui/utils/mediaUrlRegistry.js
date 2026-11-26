/** Keep object URLs alive while a committed transcript or in-flight restore owns them. */
export function createMediaUrlRegistry({
  createObjectURL = blob => URL.createObjectURL(blob),
  revokeObjectURL = url => URL.revokeObjectURL(url),
} = {}) {
  const urls = new Map();
  const loads = new Map();
  const leases = new Map();
  const pendingByKey = new Map();
  const activeByOwner = new Map();
  const activeOwnersByKey = new Map();
  let nextLeaseId = 0;
  let disposed = false;

  function ownersFor(key) {
    let owners = activeOwnersByKey.get(key);
    if (!owners) {
      owners = new Set();
      activeOwnersByKey.set(key, owners);
    }
    return owners;
  }

  function pendingFor(key) {
    let leaseIds = pendingByKey.get(key);
    if (!leaseIds) {
      leaseIds = new Set();
      pendingByKey.set(key, leaseIds);
    }
    return leaseIds;
  }

  function cleanupKey(key) {
    if (activeOwnersByKey.get(key)?.size || pendingByKey.get(key)?.size) return;
    activeOwnersByKey.delete(key);
    pendingByKey.delete(key);
    const url = urls.get(key);
    if (url) {
      urls.delete(key);
      revokeObjectURL(url);
    }
  }

  function removePendingLease(lease) {
    for (const key of lease.keys) {
      const pending = pendingByKey.get(key);
      pending?.delete(lease.id);
      if (pending?.size === 0) pendingByKey.delete(key);
    }
    leases.delete(lease.id);
    lease.closed = true;
  }

  function begin(owner) {
    const ownerId = String(owner || "").trim();
    if (disposed || !ownerId) return null;
    const lease = { id: ++nextLeaseId, owner: ownerId, keys: new Set(), closed: false };
    leases.set(lease.id, lease);
    return lease;
  }

  function acquire(lease, resourceId, loadBlob) {
    const id = String(resourceId || "").trim();
    if (disposed || !lease || lease.closed || !leases.has(lease.id) || !id) return Promise.resolve("");
    const key = `${lease.owner}\0${id}`;
    lease.keys.add(key);
    pendingFor(key).add(lease.id);
    if (urls.has(key)) return Promise.resolve(urls.get(key));
    if (loads.has(key)) return loads.get(key);

    const loading = Promise.resolve()
      .then(loadBlob)
      .then(blob => {
        const url = createObjectURL(blob);
        urls.set(key, url);
        loads.delete(key);
        cleanupKey(key);
        return url;
      })
      .catch(error => {
        loads.delete(key);
        throw error;
      });
    loads.set(key, loading);
    return loading;
  }

  function commit(lease, retainedUrls = []) {
    if (!lease || lease.closed || leases.get(lease.id) !== lease) return false;
    const previous = activeByOwner.get(lease.owner) || new Set();
    removePendingLease(lease);
    const next = new Set(lease.keys);
    const retained = new Set((Array.isArray(retainedUrls) ? retainedUrls : [])
      .map(url => String(url || "").trim())
      .filter(Boolean));
    if (retained.size) {
      for (const key of previous) {
        if (retained.has(urls.get(key))) next.add(key);
      }
    }
    activeByOwner.set(lease.owner, next);
    for (const key of previous) {
      if (next.has(key)) continue;
      const owners = activeOwnersByKey.get(key);
      owners?.delete(lease.owner);
      if (owners?.size === 0) activeOwnersByKey.delete(key);
      cleanupKey(key);
    }
    for (const key of next) ownersFor(key).add(lease.owner);
    return true;
  }

  function discard(lease) {
    if (!lease || lease.closed || leases.get(lease.id) !== lease) return false;
    const keys = [...lease.keys];
    removePendingLease(lease);
    for (const key of keys) cleanupKey(key);
    return true;
  }

  function release(owner) {
    const ownerId = String(owner || "").trim();
    const keys = activeByOwner.get(ownerId);
    if (!keys) return false;
    activeByOwner.delete(ownerId);
    for (const key of keys) {
      const owners = activeOwnersByKey.get(key);
      owners?.delete(ownerId);
      if (owners?.size === 0) activeOwnersByKey.delete(key);
      cleanupKey(key);
    }
    return true;
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    for (const url of urls.values()) revokeObjectURL(url);
    urls.clear();
    loads.clear();
    leases.clear();
    pendingByKey.clear();
    activeByOwner.clear();
    activeOwnersByKey.clear();
  }

  return { begin, acquire, commit, discard, release, dispose };
}
