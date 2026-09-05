<template>
  <n-config-provider :theme-overrides="themeOverrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider :duration="3200" :max="4">
      <n-dialog-provider>
        <AppShell />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider, zhCN, dateZhCN } from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'
import AppShell from '@/components/AppShell.vue'

// Light theme tuned to the console palette: warm neutrals, Oracle red accent.
const themeOverrides: GlobalThemeOverrides = {
  common: {
    fontFamily: 'var(--font-sans)',
    fontFamilyMono: 'var(--font-mono)',
    fontSize: '14px',
    fontSizeSmall: '13px',
    fontSizeTiny: '12px',
    fontWeightStrong: '600',
    borderRadius: '8px',
    borderRadiusSmall: '6px',
    primaryColor: '#c74634',
    primaryColorHover: '#b23e2d',
    primaryColorPressed: '#9c3527',
    primaryColorSuppl: '#c74634',
    successColor: '#1e7f4f',
    successColorHover: '#196b43',
    successColorPressed: '#145737',
    successColorSuppl: '#1e7f4f',
    warningColor: '#b7791f',
    warningColorHover: '#a06a1a',
    warningColorPressed: '#875915',
    warningColorSuppl: '#b7791f',
    errorColor: '#b42318',
    errorColorHover: '#9c1e14',
    errorColorPressed: '#851a11',
    errorColorSuppl: '#b42318',
    infoColor: '#2f6fed',
    infoColorHover: '#2860d1',
    infoColorPressed: '#2252b5',
    infoColorSuppl: '#2f6fed',
    textColorBase: '#1c1917',
    textColor1: '#1c1917',
    textColor2: '#57534e',
    textColor3: '#6f6862',
    borderColor: '#e4e1dc',
    dividerColor: '#e4e1dc',
    hoverColor: '#f4f3f1',
    cardColor: '#ffffff',
    modalColor: '#ffffff',
    popoverColor: '#ffffff',
    inputColor: '#ffffff',
    bodyColor: '#f4f3f1',
  },
  Button: {
    fontWeight: '500',
  },
  Card: {
    borderRadius: '12px',
    titleFontSizeMedium: '15px',
  },
  Dialog: {
    borderRadius: '12px',
  },
  Tabs: {
    tabFontWeightActive: '600',
  },
}
</script>
