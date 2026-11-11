import { Component, type ErrorInfo, type ReactNode } from 'react'

interface State {
  error: Error | null
}

/** Keeps a crash in one area (e.g. the map) from blanking the whole app. */
export default class ErrorBoundary extends Component<{ children: ReactNode; label: string }, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`[${this.props.label}]`, error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div role="alert" className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
        <p className="text-sm font-medium text-slate-800 dark:text-slate-100">The {this.props.label} failed to load.</p>
        <p className="max-w-md font-mono text-xs text-slate-500">{this.state.error.message}</p>
        <button className="text-sm font-medium text-sky-700 underline dark:text-sky-400" onClick={() => this.setState({ error: null })}>
          Try again
        </button>
      </div>
    )
  }
}
