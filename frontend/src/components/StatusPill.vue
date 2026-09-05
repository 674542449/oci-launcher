<template>
  <span class="pill" :class="[cls, live ? 'pill-live' : '']">{{ text }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  state?: string
  label?: string
}>()

const OK = ['RUNNING', 'ACTIVE', 'AVAILABLE', 'SUCCESS', 'SUCCEEDED', 'ATTACHED']
const BUSY = ['STARTING', 'STOPPING', 'PROVISIONING', 'CREATING', 'TERMINATING', 'UPDATING', 'MOVING', 'RESTORING', 'PENDING']
const BAD = ['TERMINATED', 'BANNED', 'INVALID', 'FAILED', 'FATAL', 'ERROR', 'FAULTY', 'DELETED']

const LABELS: Record<string, string> = {
  RUNNING: '运行中',
  STOPPED: '已停止',
  STARTING: '启动中',
  STOPPING: '停止中',
  PROVISIONING: '创建中',
  TERMINATING: '终止中',
  TERMINATED: '已终止',
  AVAILABLE: '可用',
  ACTIVE: '正常',
  BANNED: '已封禁',
  INVALID: '凭据无效',
  ERROR: '错误',
  SUCCESS: '成功',
  FAILED: '失败',
  IDLE: '空闲',
  PENDING: '等待中',
}

const norm = computed(() => (props.state || '').toUpperCase())

const cls = computed(() => {
  const s = norm.value
  if (OK.includes(s)) return 'pill-ok'
  if (BUSY.includes(s)) return 'pill-warn'
  if (BAD.includes(s)) return 'pill-danger'
  if (s === 'RUNNING_TASK') return 'pill-info'
  return 'pill-muted'
})

const live = computed(() => BUSY.includes(norm.value) || norm.value === 'RUNNING_TASK')

const text = computed(() => props.label || LABELS[norm.value] || props.state || '未知')
</script>
