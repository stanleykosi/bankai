/**
 * @description
 * Clerk Sign-In route rendered within the `(auth)` segment. Styled to mirror
 * the Bankai terminal aesthetic (mono fonts, grid background, neon accent).
 */

import { SignIn } from "@clerk/nextjs";

export default function SignInPage() {
  return (
    <div className="relative flex min-h-screen w-full flex-col items-center justify-center overflow-hidden bg-background">
      <div className="pointer-events-none absolute inset-0 opacity-20 bg-[linear-gradient(to_right,#1f1f1f_1px,transparent_1px),linear-gradient(to_bottom,#1f1f1f_1px,transparent_1px)] bg-[size:4rem_4rem] [mask-image:radial-gradient(ellipse_60%_50%_at_50%_50%,#000_70%,transparent_100%)]" />

      <div className="z-10 flex flex-col items-center gap-8 px-4">
        <div className="space-y-2 text-center">
          <h1 className="font-mono text-4xl font-bold tracking-tighter text-primary drop-shadow-[0_0_15px_rgba(41,121,255,0.5)]">
            BANKAI<span className="text-white">.TERMINAL</span>
          </h1>
          <p className="font-mono text-sm uppercase tracking-[0.3em] text-muted-foreground">
            Institutional Grade Prediction Markets
          </p>
        </div>

        <SignIn
          path="/sign-in"
          appearance={{
            elements: {
              rootBox: "w-full",
              card: "bg-card border border-border shadow-2xl backdrop-blur-sm",
              headerTitle: "text-foreground font-mono",
              headerSubtitle: "text-muted-foreground",
              socialButtonsBlockButton:
                "bg-background border border-border text-foreground hover:bg-accent hover:text-accent-foreground",
              formButtonPrimary:
                "bg-primary text-primary-foreground font-mono uppercase tracking-wide hover:bg-primary/90",
              footerActionLink: "text-primary hover:text-primary/80",
              formFieldInput: "bg-background border border-border text-foreground",
              dividerLine: "bg-border",
              dividerText: "text-muted-foreground",
            },
            variables: {
              colorPrimary: "#2979FF",
              colorBackground: "#050505",
              colorText: "#C9D1D9",
              borderRadius: "0.25rem",
            },
          }}
        />
      </div>
    </div>
  );
}

