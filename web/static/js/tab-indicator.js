/**
 * Sliding Tab Indicator Animation
 * Adds smooth sliding animation to templui tabs
 */
(function() {
  'use strict';

  // Keep lightweight retry timers per tabsId to avoid thrashing
  const retryTimers = new Map();

  function scheduleRetry(tabsId, delay = 60, tries = 5) {
    if (tries <= 0) return;
    if (retryTimers.has(tabsId)) clearTimeout(retryTimers.get(tabsId));
    const t = setTimeout(() => {
      updateTabIndicator(tabsId, false);
      // Backoff slightly for subsequent retries
      scheduleRetry(tabsId, Math.min(delay * 1.5, 250), tries - 1);
    }, delay);
    retryTimers.set(tabsId, t);
  }

  function updateTabIndicator(tabsId, triggerBounce = false, previousTab = null) {
    const tabsList = document.querySelector(`[data-tui-tabs-list][data-tui-tabs-id="${tabsId}"]`);
    const activeTrigger = document.querySelector(`[data-tui-tabs-trigger][data-tui-tabs-id="${tabsId}"][data-tui-tabs-state="active"]`);
    
    if (!tabsList) return;
    // If no active trigger yet, fall back to the first trigger and retry soon
    const trigger = activeTrigger || tabsList.querySelector('[data-tui-tabs-trigger]');
    if (!trigger) return;

    const triggers = tabsList.querySelectorAll('[data-tui-tabs-trigger]');
    const tabsListRect = tabsList.getBoundingClientRect();
    const activeTriggerRect = trigger.getBoundingClientRect();

    // Prefer layout-based measures; if width is 0 (hidden/not laid out yet), retry soon
    const width = activeTriggerRect.width || trigger.offsetWidth || trigger.scrollWidth || 0;
    if (!width || width <= 0 || !Number.isFinite(width)) {
      scheduleRetry(tabsId);
      return;
    }

    // Calculate position relative to tabs list container
    // Subtract the actual padding-left so this works for both default (3px)
    // and analytics cards (2px) without hard-coding values.
    const paddingLeft = parseFloat(getComputedStyle(tabsList).paddingLeft) || 0;
    const offset = (activeTriggerRect.left - tabsListRect.left - paddingLeft);
    if (!Number.isFinite(offset)) {
      scheduleRetry(tabsId);
      return;
    }
    
    // Get current position for direction calculation
    const currentOffset = parseFloat(tabsList.style.getPropertyValue('--tab-indicator-offset') || '0');
    
    // Update CSS custom properties for the sliding indicator
    tabsList.style.setProperty('--tab-indicator-width', `${Math.max(1, Math.round(width))}px`);
    tabsList.style.setProperty('--tab-indicator-offset', `${offset}px`);
    
    // Trigger directional breathing effect if requested
    if (triggerBounce && Math.abs(offset - currentOffset) > 5) { // Only animate if significant movement
      // Determine animation direction
      const movingRight = offset > currentOffset;
      // Let's try the opposite - testing what actually works visually
      const animationClass = movingRight ? 'tab-breathing-right' : 'tab-breathing-left';
      
      // Clear any existing animation classes
      tabsList.classList.remove('tab-breathing-left', 'tab-breathing-right');
      // Force reflow to restart animation
      void tabsList.offsetWidth;
      tabsList.classList.add(animationClass);
      
      // Remove the class after animation completes
      setTimeout(() => {
        tabsList.classList.remove(animationClass);
      }, 350); // Match the animation duration
    }
  }

  function initializeTabIndicators() {
    // Initialize all tabs on page load
    document.querySelectorAll('[data-tui-tabs]').forEach(tabsContainer => {
      const tabsId = tabsContainer.getAttribute('data-tui-tabs-id');
      if (tabsId) {
        updateTabIndicator(tabsId);
      }
    });
  }

  // Handle tab clicks to animate indicator
  document.addEventListener('click', function(event) {
    const trigger = event.target.closest('[data-tui-tabs-trigger]');
    if (!trigger) return;

    const tabsId = trigger.getAttribute('data-tui-tabs-id');
    const wasActive = trigger.getAttribute('data-tui-tabs-state') === 'active';
    
    // Only animate if clicking a different tab
    if (!wasActive) {
      // Small delay to allow templui to update states first
      setTimeout(() => {
        updateTabIndicator(tabsId, true); // true = trigger directional animation
        
        // If this seems to be a main analytics tab, also recalculate nested tabs
        const tabValue = trigger.getAttribute('data-tui-tabs-value');
        if (tabValue && ['overview', 'metrics', 'qr', 'activity'].includes(tabValue)) {
          // Give extra time for content to become visible, then fix nested tabs
          setTimeout(handleTabContentVisibilityChange, 400);
        }
      }, 10);
    }
  });

  // Handle window resize to recalculate positions
  window.addEventListener('resize', function() {
    document.querySelectorAll('[data-tui-tabs]').forEach(tabsContainer => {
      const tabsId = tabsContainer.getAttribute('data-tui-tabs-id');
      if (tabsId) {
        updateTabIndicator(tabsId, false); // No animation on resize
      }
    });
  });

  // Listen for transition end events on tab content
  document.addEventListener('transitionend', function(event) {
    // Check if this is a tab content element that just finished transitioning
    if (event.target.hasAttribute('data-tui-tabs-content') && 
        event.propertyName === 'opacity' &&
        event.target.getAttribute('data-tui-tabs-state') === 'active') {
      // Tab content just became fully visible - recalculate any nested tabs
      setTimeout(handleTabContentVisibilityChange, 50);
    }
  });

  // Initialize when DOM is ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
      // Defer one tick so templui tabs can mark active states first
      requestAnimationFrame(() => initializeTabIndicators());
    });
  } else {
    // DOM already parsed; still defer one frame
    requestAnimationFrame(() => initializeTabIndicators());
  }

  // As a safety net, run again after full load and nudge layout to recalc
  window.addEventListener('load', () => {
    // Run twice to cover late layout shifts/fonts
    initializeTabIndicators();
    setTimeout(initializeTabIndicators, 80);
    // Resize event forces our resize handler to recompute offsets
    setTimeout(() => window.dispatchEvent(new Event('resize')), 100);
  });

  // Handle visibility changes for tab content (when tabs become visible)
  function handleTabContentVisibilityChange() {
    // Look for tab content that just became visible
    document.querySelectorAll('[data-tui-tabs-content][data-tui-tabs-state="active"]').forEach(tabContent => {
      // Find any nested tabs inside this newly visible content
      const nestedTabs = tabContent.querySelectorAll('[data-tui-tabs]');
      nestedTabs.forEach(nestedTab => {
        const tabsId = nestedTab.getAttribute('data-tui-tabs-id');
        if (tabsId) {
          // Recalculate indicator position for nested tabs
          setTimeout(() => updateTabIndicator(tabsId, false), 100); // No animation, just reposition
        }
      });
    });
  }

  // Handle dynamic content changes
  const observer = new MutationObserver(function(mutations) {
    mutations.forEach(function(mutation) {
      if (mutation.type === 'childList') {
        mutation.addedNodes.forEach(function(node) {
          if (node.nodeType === Node.ELEMENT_NODE) {
            // Check if new tabs were added
            if (node.querySelector && node.querySelector('[data-tui-tabs]')) {
              setTimeout(initializeTabIndicators, 50);
            }
          }
        });
      }
      // Handle attribute changes for tab state
      if (mutation.type === 'attributes' && 
          mutation.attributeName === 'data-tui-tabs-state' &&
          mutation.target.getAttribute('data-tui-tabs-trigger') !== null) {
        const tabsId = mutation.target.getAttribute('data-tui-tabs-id');
        const newState = mutation.target.getAttribute('data-tui-tabs-state');
        if (tabsId && newState === 'active') {
          setTimeout(() => updateTabIndicator(tabsId, true), 10); // true = trigger directional animation
        }
      }
      // Handle tab content becoming visible/hidden
      if (mutation.type === 'attributes' && 
          mutation.attributeName === 'data-tui-tabs-state' &&
          mutation.target.getAttribute('data-tui-tabs-content') !== null) {
        const newState = mutation.target.getAttribute('data-tui-tabs-state');
        if (newState === 'active') {
          // Tab content just became visible - check for nested tabs
          setTimeout(handleTabContentVisibilityChange, 150);
        }
      }
    });
  });

  observer.observe(document.body, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ['data-tui-tabs-state']
  });
})();
