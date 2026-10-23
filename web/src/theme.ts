import { theme, type ThemeConfig } from 'antd'

// Design "A": neutral, high-contrast, black primary actions.
const dayColors = {
  ink: '#111111', muted: '#666666', faint: '#8f8f8f', placeholder: '#a3a3a3',
  primary: '#111111', onPrimary: '#ffffff', link: '#0b63ce', brand: '#111111',
  border: '#eaeaea', borderStrong: '#d6d6d6', borderHover: '#b5b5b5',
  layout: '#ffffff', container: '#ffffff', elevated: '#ffffff',
  surface: '#fafafa', chrome: '#ffffff', chromeTint: '#fafafa', spotlight: '#111111',
  selected: '#f2f2f2', selectedBorder: '#e6e6e6', selectedText: '#111111',
  hover: '#f4f4f4', focus: '#0b63ce',
  success: '#16a34a', warning: '#d97706', error: '#dc2626',
  warningText: '#a35a00', errorText: '#b91c1c',
  download: '#111111', upload: '#9a9a9a',
  shadow: '0 0 0',
}
export const FONT = '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif'

export type ConsoleThemeId = 'blue' | 'dark'
export type ConsoleThemePreference = ConsoleThemeId | 'auto'

// The login screen renders before a theme preference is available, so it always
// uses the day palette. Taking the accent from dayColors keeps the primary
// button in the same blue as the rest of the panel instead of a second,
// hardcoded brand colour.
export const loginTheme: ThemeConfig = {
  token: {
    colorPrimary: dayColors.primary,
    colorText: dayColors.ink,
    colorTextPlaceholder: dayColors.placeholder,
    colorBorder: dayColors.borderStrong,
    borderRadius: 8,
    controlHeightLG: 42,
    fontSize: 14,
    fontFamily: FONT,
  },
  components: { Button: { primaryShadow: 'none', fontWeight: 500 } },
}

export type ConsolePalette = {
  id: ConsoleThemeId
  name: string
  colors: Record<keyof typeof dayColors, string>
}

export const consolePalettes: ConsolePalette[] = [
  // Keep the existing ID so saved classic preferences become the day theme.
  { id: 'blue', name: '日间', colors: dayColors },
  {
    id: 'dark', name: '夜间',
    colors: {
      // Neutral greys (not pure black), close to the browser's own dark chrome.
      ink: '#ececec', muted: '#a8a8a8', faint: '#868686', placeholder: '#7a7a7a',
      primary: '#ececec', onPrimary: '#1e1e1e', link: '#7cb0ff', brand: '#ececec',
      border: '#353535', borderStrong: '#474747', borderHover: '#5e5e5e',
      layout: '#1e1e1e', container: '#1e1e1e', elevated: '#2a2a2a',
      surface: '#252525', chrome: '#1e1e1e', chromeTint: '#252525', spotlight: '#3a3a3a',
      selected: '#333333', selectedBorder: '#3d3d3d', selectedText: '#ececec',
      hover: '#2a2a2a', focus: '#7cb0ff',
      success: '#4cc46a', warning: '#e8a94a', error: '#f27a7c',
      warningText: '#e8a94a', errorText: '#f27a7c',
      download: '#ececec', upload: '#868686', shadow: '0 0 0',
    },
  },
]

export const DEFAULT_CONSOLE_THEME: ConsoleThemeId = 'blue'
export const DEFAULT_USER_CONSOLE_THEME: ConsoleThemeId = DEFAULT_CONSOLE_THEME
export const userConsolePalettes = consolePalettes

export function parseConsoleTheme(value: unknown): ConsoleThemePreference {
  if (value === 'auto') return 'auto'
  return consolePalettes.find((p) => p.id === value)?.id ?? DEFAULT_CONSOLE_THEME
}

export function parseUserConsoleTheme(value: unknown): ConsoleThemePreference {
  return parseConsoleTheme(value)
}

export function getConsolePalette(id: ConsoleThemeId): ConsolePalette {
  return consolePalettes.find((p) => p.id === id) ?? consolePalettes[0]
}

export function consoleCssVariables(palette: ConsolePalette): Record<string, string> {
  return Object.fromEntries(Object.entries(palette.colors).map(([name, value]) => [
    `--console-${name.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)}`, value,
  ]))
}

export function getConsoleTheme(palette: ConsolePalette): ThemeConfig {
  const c = palette.colors
  return {
    algorithm: palette.id === 'dark' ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: c.primary, colorInfo: c.link,
      colorPrimaryBg: c.selected, colorPrimaryBgHover: c.selectedBorder,
      controlItemBgActive: c.selected, controlItemBgActiveHover: c.selectedBorder,
      controlItemBgHover: c.hover,
      colorSuccess: c.success, colorWarning: c.warning, colorError: c.error,
      colorLink: c.link, colorLinkHover: c.primary,
      colorText: c.ink, colorTextSecondary: c.muted, colorTextTertiary: c.faint,
      colorTextPlaceholder: c.placeholder,
      colorBgLayout: c.layout, colorBgContainer: c.container, colorBgElevated: c.elevated,
      colorBorder: c.borderStrong, colorBorderSecondary: c.border,
      colorFillAlter: c.surface, colorFillSecondary: c.selected,
      borderRadius: 6, borderRadiusLG: 12, borderRadiusSM: 4, controlHeight: 32, controlHeightSM: 26, fontSize: 14, fontFamily: FONT,
      colorPrimaryHover: palette.id === 'dark' ? '#d4d4d4' : '#333333', colorPrimaryActive: c.primary,
      boxShadow: `0 8px 28px rgb(${c.shadow} / 8%)`,
      boxShadowSecondary: `0 12px 40px rgb(${c.shadow} / 12%)`,
      motionDurationFast: '0.12s', motionDurationMid: '0.2s', motionDurationSlow: '0.28s',
    },
    components: {
      Layout: { bodyBg: c.layout, headerBg: c.chrome, lightSiderBg: c.chrome },
      Button: {
        primaryShadow: 'none', defaultShadow: 'none', dangerShadow: 'none', fontWeight: 500,
        primaryColor: c.onPrimary, defaultBorderColor: c.border,
        defaultHoverBorderColor: c.borderHover, defaultHoverColor: c.ink, defaultHoverBg: c.container,
      },
      Card: { headerFontSize: 14, headerHeight: 48 },
      Table: {
        headerBg: c.surface, headerColor: c.muted, headerSplitColor: 'transparent',
        rowHoverBg: c.surface, rowSelectedBg: c.selected, rowSelectedHoverBg: c.hover,
        borderColor: c.border, cellPaddingBlock: 13, cellPaddingInline: 16, headerBorderRadius: 12,
      },
      Tabs: { inkBarColor: c.ink, itemSelectedColor: c.ink, itemHoverColor: c.ink, itemColor: c.muted },
      Segmented: { trackPadding: 2, trackBg: c.surface, itemSelectedBg: palette.id === 'dark' ? '#3a3a3a' : '#ffffff', itemSelectedColor: c.ink, itemColor: c.muted },
      Select: { optionSelectedBg: c.selected, optionActiveBg: c.hover, activeBorderColor: c.borderHover, hoverBorderColor: c.borderHover, activeOutlineColor: 'transparent' },
      Modal: { titleFontSize: 16 },
      Input: { activeShadow: 'none', activeBorderColor: c.borderHover, hoverBorderColor: c.borderHover },
      InputNumber: { activeShadow: 'none', activeBorderColor: c.borderHover, hoverBorderColor: c.borderHover },
      DatePicker: { activeShadow: 'none', activeBorderColor: c.borderHover, hoverBorderColor: c.borderHover },
      Switch: { colorPrimary: c.ink, colorPrimaryHover: palette.id === 'dark' ? '#d4d4d4' : '#333333' },
      Progress: { defaultColor: c.ink, remainingColor: c.border },
      Statistic: { contentFontSize: 24, titleFontSize: 13 },
      Tooltip: { colorBgSpotlight: c.spotlight },
      Tag: { defaultBg: 'transparent', defaultColor: c.muted },
    },
  }
}
