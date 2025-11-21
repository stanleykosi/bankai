import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: ["class"],
  content: [
    "./pages/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./app/**/*.{ts,tsx}",
    "./src/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ["var(--font-inter)"],
        mono: ["var(--font-mono)"],
      },
      colors: {
        // Bankai Theme Colors (from spec)
        background: "#050505", // OLED Black
        foreground: "#C9D1D9", // Light grey text
        card: "#121212", // Glass/Blur panels
        
        primary: {
          DEFAULT: "#2979FF", // Electric Blue
          foreground: "#FFFFFF",
        },
        
        // Trading-specific colors
        constructive: "#00E676", // Neon Green (YES/Profit)
        destructive: "#FF1744", // Neon Red (NO/Loss)
        
        // Additional UI colors
        secondary: "#8B949E", // Grey for secondary text
        muted: {
          DEFAULT: "#161B22",
          foreground: "#8B949E",
        },
        border: "#30363D",
        input: "#21262D",
        ring: "#2979FF",
      },
      borderRadius: {
        lg: "0.5rem",
        md: "0.375rem",
        sm: "0.25rem",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};

export default config;

