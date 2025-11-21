/**
 * @description
 * Axios instance configuration for frontend API communication.
 * Handles base URL setup and default headers.
 *
 * @dependencies
 * - axios
 */

import axios from 'axios';

const rawApiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const API_URL = rawApiUrl.replace(/\/+$/, '');

export const api = axios.create({
  baseURL: `${API_URL}/api/v1`,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Add request interceptor to inject auth token if available
api.interceptors.request.use(async (config) => {
  // We will handle Clerk token injection here in future steps if needed,
  // usually passed via function arguments or retrieved from window/storage if appropriate,
  // but typical Clerk usage involves getting the token via hook and passing it in headers explicitly.
  return config;
});

export default api;

