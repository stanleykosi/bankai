/** @type {import('next').NextConfig} */
const nextConfig = {
  webpack: (config) => {
    config.resolve.fallback = config.resolve.fallback || {};
    // Stub out optional deps not needed in the browser build.
    config.resolve.fallback["@react-native-async-storage/async-storage"] = false;
    config.resolve.fallback["pino-pretty"] = false;
    config.resolve.fallback["node-fetch"] = false;
    // Ensure node-fetch resolutions are treated as empty to placate pack file cache checks.
    config.resolve.alias = config.resolve.alias || {};
    config.resolve.alias["node-fetch"] = false;
    return config;
  },
};

module.exports = nextConfig;
