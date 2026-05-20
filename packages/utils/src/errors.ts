/**
 * Application error with a stable code and optional structured data. Used
 * for both wire-level errors (returned in `Envelope.error`) and local
 * client-side failures.
 */
export class AppError extends Error {
  public override readonly name = 'AppError';
  public readonly code: string;
  public readonly data?: unknown;

  constructor(code: string, message: string, data?: unknown) {
    super(message);
    this.code = code;
    this.data = data;
  }
}

/** Type guard. */
export function isAppError(value: unknown): value is AppError {
  return value instanceof AppError;
}
