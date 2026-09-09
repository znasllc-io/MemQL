// One lock service shared by independent document/auth-source instances.
// Each callback holds the lock until its response completes, like Web Locks.
export function installSharedWebLocks(): void {
  const tails = new Map<string, Promise<unknown>>();
  Object.defineProperty(navigator, "locks", { configurable: true, value: {
    request<T>(name: string, _options: LockOptions, callback: () => Promise<T>): Promise<T> {
      const turn = (tails.get(name) ?? Promise.resolve()).catch(() => {}).then(callback);
      tails.set(name, turn);
      return turn;
    },
  } });
}
