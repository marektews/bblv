// Retry runs a function again after it failed, at the given delays in turn;
// once they are used up it waits for reset, e.g. after the connection to
// the server came back.
export class Retry {
  private timer: ReturnType<typeof setTimeout> | undefined
  private attempt = 0

  constructor(
    private readonly delays: number[],
    private readonly run: () => void,
  ) {}

  // failed schedules the next attempt, if any is left.
  failed() {
    this.cancel()
    if (this.attempt < this.delays.length) {
      this.timer = setTimeout(this.run, this.delays[this.attempt++])
    }
  }

  // reset forgets the attempts made, e.g. for another file.
  reset() {
    this.cancel()
    this.attempt = 0
  }

  cancel() {
    clearTimeout(this.timer)
    this.timer = undefined
  }
}
