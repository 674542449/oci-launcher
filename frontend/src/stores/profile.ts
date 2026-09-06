import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'

export interface OCIProfile {
  id: number
  name: string
  tenancy_ocid: string
  user_ocid: string
  fingerprint: string
  region: string
  account_type_override: string
  detected_type: string
  detection_reason: string
  detection_source?: string
  account_email?: string
  account_created_at?: string | null
  tenancy_name?: string
  country_code?: string
  status: string
  status_message: string
  tags: string
  notes: string
  is_active: boolean
  default_ssh_key_id?: number
}

export const useProfileStore = defineStore('profile', () => {
  const profiles = ref<OCIProfile[]>([])
  const activeProfileId = ref<number | null>(null)
  const loading = ref(false)

  const fetchProfiles = async () => {
    loading.value = true
    try {
      const res: any = await api.get('/profiles')
      profiles.value = res.profiles || []

      // If activeProfileId is null, load from localStorage or default to first
      const saved = localStorage.getItem('oci_active_profile_id')
      if (saved && profiles.value.some(p => p.id === Number(saved))) {
        activeProfileId.value = Number(saved)
      } else if (profiles.value.length > 0) {
        activeProfileId.value = profiles.value[0].id
        localStorage.setItem('oci_active_profile_id', String(profiles.value[0].id))
      }
    } catch (e) {
      console.error(e)
    } finally {
      loading.value = false
    }
  }

  const setActiveProfile = (id: number) => {
    activeProfileId.value = id
    localStorage.setItem('oci_active_profile_id', String(id))
  }

  return {
    profiles,
    activeProfileId,
    loading,
    fetchProfiles,
    setActiveProfile,
  }
})
