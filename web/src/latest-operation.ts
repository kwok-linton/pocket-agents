/** Only the current selection may receive an asynchronous operation's callbacks. */
export function latestOperation() {
  let epoch = 0;
  let pending = false;
  return {
    get pending() {
      return pending;
    },
    invalidate() {
      epoch++;
      pending = false;
    },
    async run<T>(
      work: () => Promise<T>,
      callbacks: {
        success: (value: T) => void;
        error: (error: unknown) => void;
        settled: () => void;
      },
    ) {
      const requestEpoch = ++epoch;
      pending = true;
      try {
        const value = await work();
        if (requestEpoch === epoch) callbacks.success(value);
      } catch (error) {
        if (requestEpoch === epoch) callbacks.error(error);
      } finally {
        if (requestEpoch === epoch) {
          pending = false;
          callbacks.settled();
        }
      }
    },
  };
}
