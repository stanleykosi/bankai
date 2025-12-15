/**
 * Installs a console redaction wrapper to avoid leaking sensitive
 * credentials in browser logs (e.g., CLOB client request errors that
 * serialize headers). Only string arguments are processed.
 */
let installed = false;
let originalError: typeof console.error | null = null;
let originalWarn: typeof console.warn | null = null;

const redactString = (input: string) => {
  // Redact POLY_* headers and similar credential-looking values
  const patterns: Array<{
    regex: RegExp;
    replacer: (match: string, key: string) => string;
  }> = [
    {
      regex: /"(POLY_[A-Z_]+)":"[^"]+"/g, // JSON key/value pairs
      replacer: (_match, key) => `"${String(key)}":"[REDACTED]"`,
    },
    {
      regex: /(POLY_[A-Z_]+)=([A-Za-z0-9._\-]+)/g, // key=value pairs
      replacer: (_match, key) => `${String(key)}=[REDACTED]`,
    },
  ];

  return patterns.reduce(
    (acc, { regex, replacer }) => acc.replace(regex, replacer),
    input
  );
};

const redactArg = (arg: any) => {
  if (typeof arg === "string") {
    return redactString(arg);
  }
  return arg;
};

export const installLogRedaction = () => {
  if (installed || typeof console === "undefined") return;

  // Capture originals once to avoid recursive calls
  originalError = console.error.bind(console);
  originalWarn = console.warn.bind(console);

  const wrap =
    (orig: (...a: any[]) => void) =>
    (...args: any[]) => {
      try {
        orig(...args.map(redactArg));
      } catch {
        // In case redaction itself fails, fall back to the original log
        orig(...args);
      }
    };

  console.error = wrap(originalError) as typeof console.error;
  console.warn = wrap(originalWarn) as typeof console.warn;

  installed = true;
};
