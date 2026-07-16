import { useEffect, useState } from 'react';

function readLayoutState() {
  if (typeof window === 'undefined') {
    return {
      isExtraSmall: false,
      isMobile: false,
      isTablet: false,
      isCompactDesktop: false,
    };
  }

  const width = window.innerWidth;
  return {
    isExtraSmall: width <= 480,
    isMobile: width <= 768,
    isTablet: width > 768 && width <= 1024,
    isCompactDesktop: width <= 1024,
  };
}

export function usePlaygroundLayoutState(historyLength: number) {
  const [focusMode, setFocusMode] = useState(false);
  const [layoutState, setLayoutState] = useState(() => readLayoutState());

  useEffect(() => {
    const handleResize = () => {
      setLayoutState(readLayoutState());
    };

    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setFocusMode(false);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const showDesktopSettingsRail = !focusMode && !layoutState.isMobile && !layoutState.isCompactDesktop;
  const showDesktopHistoryRail = showDesktopSettingsRail && historyLength > 0;
  const playgroundGridTemplateColumns = focusMode || layoutState.isMobile || layoutState.isCompactDesktop
    ? '1fr'
    : showDesktopHistoryRail
      ? '252px minmax(0, 1fr) 236px'
      : '252px minmax(0, 1fr)';
  const promptHeight = layoutState.isExtraSmall ? 100 : layoutState.isMobile ? 116 : 132;

  return {
    ...layoutState,
    focusMode,
    setFocusMode,
    promptHeight,
    showDesktopSettingsRail,
    showDesktopHistoryRail,
    playgroundGridTemplateColumns,
  };
}
