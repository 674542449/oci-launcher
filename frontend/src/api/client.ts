import axios from 'axios'

export const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
  withCredentials: true, // Send HttpOnly Cookie
  headers: {
    'Content-Type': 'application/json',
    'X-Requested-With': 'XMLHttpRequest',
  },
})

const readToken = () => {
  try {
    return localStorage.getItem('token') || ''
  } catch {
    return ''
  }
}
const writeToken = (token: string) => {
  try {
    localStorage.setItem('token', token)
  } catch {
    /* storage unavailable: the HttpOnly cookie still carries the session */
  }
}

// Request interceptor
api.interceptors.request.use((config) => {
  const token = readToken()
  if (token && !config.headers.Authorization) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor
api.interceptors.response.use(
  (response) => {
    // Sliding session: the backend reissues the token once it is older than an hour.
    const refreshed = response.headers?.['x-refreshed-token']
    if (typeof refreshed === 'string' && refreshed) writeToken(refreshed)
    return response.data
  },
  (error) => {
    const status = error.response?.status
    if (status === 401) {
      // A request that left with an older token can fail while a renewed one is already stored
      // (parallel requests around a renewal): retry it once with the current token.
      const config = error.config || {}
      const used = String(config.headers?.Authorization || '').replace(/^Bearer /, '')
      const current = readToken()
      if (current && used && current !== used && !config.__retried) {
        config.__retried = true
        config.headers.Authorization = `Bearer ${current}`
        return api.request(config)
      }
      if (window.location.pathname !== '/login') {
        try {
          localStorage.removeItem('token')
        } catch {
          /* ignore */
        }
        window.location.href = '/login?expired=1'
      }
    }
    const msg = error.response?.data?.error || error.message || '网络请求错误'
    return Promise.reject(new Error(msg))
  },
)
