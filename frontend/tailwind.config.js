/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        oci: {
          red: '#C74634',
          dark: '#161513',
          gray: '#F5F5F7',
          border: '#E5E7EB',
          accent: '#0284C7',
        }
      }
    },
  },
  plugins: [],
}
