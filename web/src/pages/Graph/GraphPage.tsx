import { useEffect, useRef, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import cytoscape from 'cytoscape';
import fcose from 'cytoscape-fcose';
import { getGraph } from '../../api/client';
import { useProjectStore } from '../../stores/projectStore';
import { useUIStore } from '../../stores/uiStore';
import { useToastStore } from '../../components/Toast/ToastContainer';
import { getProjectColor } from '../../lib/tagColor';
import { GraphSkeleton } from '../../components/Skeleton/Skeleton';
import type { GraphData } from '../../api/types';
import styles from './GraphPage.module.css';

// Guard against double-registration during hot-reload (cytoscape throws if
// the same extension is registered twice).
try {
  cytoscape.use(fcose);
} catch {
  // Already registered
}

// Design token values extracted from variables.css so Cytoscape
// (which cannot read CSS custom properties) stays in sync.
const COLORS = {
  accentPrimary: '#c4915c',
  accentMuted: 'rgba(196, 145, 92, 0.10)',
  borderDefault: '#2a3045',
  borderSubtle: '#1e2233',
  textPrimary: '#e8e2d9',
  bgSurface: '#161922',
} as const;

function hexToRgba(hex: string, alpha: number): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

export function GraphPage() {
  const navigate = useNavigate();
  const containerRef = useRef<HTMLDivElement>(null);
  const minimapRef = useRef<HTMLDivElement>(null);
  const cyRef = useRef<cytoscape.Core | null>(null);
  const minimapCyRef = useRef<cytoscape.Core | null>(null);
  const projects = useProjectStore((s) => s.projects);
  const tags = useUIStore((s) => s.tags);

  const [graphData, setGraphData] = useState<GraphData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tooltip, setTooltip] = useState<{ text: string; x: number; y: number } | null>(null);
  const [selectedProjects, setSelectedProjects] = useState<Set<string>>(
    new Set(),
  );
  const [activeTags, setActiveTags] = useState<Set<string>>(new Set());
  const [sinceDate, setSinceDate] = useState('');
  const [untilDate, setUntilDate] = useState('');
  const [focusedNodeIndex, setFocusedNodeIndex] = useState(-1);
  const addToast = useToastStore((s) => s.addToast);

  // Build project color map.
  const projectColorMap = useCallback(() => {
    const map = new Map<string, string>();
    projects.forEach((p, i) => {
      map.set(p.id, getProjectColor(i));
    });
    return map;
  }, [projects]);

  // Fetch graph data (no tag/project filters -- all filtering is client-side).
  const fetchGraph = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getGraph({
        since: sinceDate ? new Date(sinceDate).toISOString() : undefined,
        until: untilDate ? new Date(untilDate).toISOString() : undefined,
      });
      setGraphData(data);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load graph data';
      setError(message);
      addToast(message, 'error');
    } finally {
      setLoading(false);
    }
  }, [sinceDate, untilDate, addToast]);

  useEffect(() => {
    fetchGraph();
  }, [fetchGraph]);

  // Initialize and update cytoscape.
  useEffect(() => {
    if (!containerRef.current || !graphData) return;

    const colors = projectColorMap();

    // Build elements.
    const nodes = graphData.nodes.map((node) => {
      const color = node.project_id
        ? (colors.get(node.project_id) ?? COLORS.accentPrimary)
        : COLORS.accentPrimary;
      const size = Math.min(24 + (node.link_count ?? 0) * 4, 48);
      return {
        data: {
          id: node.id,
          label: node.title,
          projectId: node.project_id || '',
          color,
          size,
          tags: node.tags || [],
        },
      };
    });

    const edges = graphData.edges.map((edge, i) => ({
      data: {
        id: `e${i}`,
        source: edge.source,
        target: edge.target,
      },
    }));

    // Destroy previous instances.
    if (cyRef.current) {
      cyRef.current.destroy();
    }
    if (minimapCyRef.current) {
      minimapCyRef.current.destroy();
    }

    const prefersReducedMotion =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    const cy = cytoscape({
      container: containerRef.current,
      elements: [...nodes, ...edges],
      style: [
        {
          selector: 'node',
          style: {
            'background-color': ((ele: cytoscape.NodeSingular) =>
              hexToRgba(ele.data('color'), 0.15)) as unknown as string,
            'border-width': 1.5,
            'border-color': ((ele: cytoscape.NodeSingular) =>
              hexToRgba(ele.data('color'), 0.6)) as unknown as string,
            label: 'data(label)',
            'font-family': 'Outfit, system-ui, sans-serif',
            'font-size': '12px',
            'font-weight': 400,
            color: COLORS.textPrimary,
            'text-valign': 'bottom',
            'text-halign': 'center',
            'text-margin-y': 6,
            width: 'data(size)',
            height: ((ele: cytoscape.NodeSingular) =>
              Math.max(ele.data('size') * 0.6, 20)) as unknown as number,
            shape: 'round-rectangle',
            'text-max-width': '100px',
            'text-wrap': 'ellipsis',
          } as cytoscape.Css.Node,
        },
        {
          selector: 'node:selected',
          style: {
            'border-color': COLORS.accentPrimary,
            'background-color': COLORS.accentMuted,
            'border-width': 2,
          } as cytoscape.Css.Node,
        },
        {
          selector: 'edge',
          style: {
            'line-color': COLORS.borderDefault,
            width: 1,
            opacity: 0.4,
            'curve-style': 'bezier',
            'target-arrow-shape': 'triangle',
            'target-arrow-color': COLORS.borderDefault,
            'arrow-scale': 0.6,
          } as cytoscape.Css.Edge,
        },
        {
          selector: 'edge:selected',
          style: {
            'line-color': COLORS.accentPrimary,
            'target-arrow-color': COLORS.accentPrimary,
            width: 2,
            opacity: 1,
          } as cytoscape.Css.Edge,
        },
      ],
      layout: {
        name: 'fcose',
        animate: !prefersReducedMotion,
        animationDuration: 500,
        quality: 'default',
        nodeSeparation: 100,
        idealEdgeLength: 120,
        nodeRepulsion: () => 8000,
        gravity: 0.3,
      } as cytoscape.LayoutOptions,
      minZoom: 0.2,
      maxZoom: 4,
      wheelSensitivity: 0.3,
    });

    // Click to select node, click background to deselect.
    cy.on('tap', (evt) => {
      if (evt.target === cy) {
        cy.elements().unselect();
      }
    });

    // Double-click to navigate.
    cy.on('dbltap', 'node', (evt) => {
      const nodeId = evt.target.id();
      navigate(`/notes/${nodeId}`);
    });

    // Hover: highlight connected edges and show tooltip.
    cy.on('mouseover', 'node', (evt) => {
      const node = evt.target;
      node.style({
        'border-color': node.data('color'),
        'background-color': hexToRgba(node.data('color'), 0.25),
        'font-weight': 500,
      } as cytoscape.Css.Node);
      node.connectedEdges().style({
        'line-color': COLORS.accentPrimary,
        'target-arrow-color': COLORS.accentPrimary,
        width: 2,
        opacity: 1,
      } as cytoscape.Css.Edge);
      // Show tooltip with note title and link count.
      const pos = node.renderedPosition();
      const linkCount = node.connectedEdges().length;
      setTooltip({
        text: `${node.data('label')} (${linkCount} ${linkCount === 1 ? 'link' : 'links'})`,
        x: pos.x,
        y: pos.y - 30,
      });
    });

    cy.on('mouseout', 'node', (evt) => {
      const node = evt.target;
      node.style({
        'border-color': hexToRgba(node.data('color'), 0.6),
        'background-color': hexToRgba(node.data('color'), 0.15),
        'font-weight': 400,
      } as cytoscape.Css.Node);
      node.connectedEdges().style({
        'line-color': COLORS.borderDefault,
        'target-arrow-color': COLORS.borderDefault,
        width: 1,
        opacity: 0.4,
      } as cytoscape.Css.Edge);
      setTooltip(null);
    });

    cyRef.current = cy;

    // Create minimap (secondary small cytoscape instance).
    if (minimapRef.current) {
      const minimapCy = cytoscape({
        container: minimapRef.current,
        elements: cy.elements().jsons(),
        style: [
          {
            selector: 'node',
            style: {
              'background-color': COLORS.accentPrimary,
              width: 4,
              height: 4,
              label: '',
            } as cytoscape.Css.Node,
          },
          {
            selector: 'edge',
            style: {
              'line-color': COLORS.borderSubtle,
              width: 0.5,
              opacity: 0.3,
            } as cytoscape.Css.Edge,
          },
        ],
        layout: { name: 'preset' },
        userZoomingEnabled: false,
        userPanningEnabled: false,
        boxSelectionEnabled: false,
        autoungrabify: true,
        autounselectify: true,
      });

      minimapCy.fit(undefined, 4);
      minimapCyRef.current = minimapCy;

      // Update minimap viewport rectangle on pan/zoom/position changes.
      // Debounced to avoid jank on large graphs (runs at most every 50ms).
      let minimapTimer: ReturnType<typeof setTimeout> | undefined;
      const updateMinimapImmediate = () => {
        if (!minimapCyRef.current || !cyRef.current) return;
        const mc = minimapCyRef.current;

        // Sync node positions from main graph to minimap.
        cyRef.current.nodes().forEach((node) => {
          const pos = node.position();
          const minimapNode = mc.getElementById(node.id());
          if (minimapNode.length > 0) {
            minimapNode.position(pos);
          }
        });
        mc.fit(undefined, 4);

        // Draw viewport rectangle using an overlay node.
        const ext = cyRef.current.extent();
        const viewportId = '__viewport_rect__';
        const existing = mc.getElementById(viewportId);
        if (existing.length > 0) {
          existing.position({
            x: (ext.x1 + ext.x2) / 2,
            y: (ext.y1 + ext.y2) / 2,
          });
          existing.style({
            width: ext.x2 - ext.x1,
            height: ext.y2 - ext.y1,
          });
        } else {
          mc.add({
            group: 'nodes',
            data: { id: viewportId },
            position: {
              x: (ext.x1 + ext.x2) / 2,
              y: (ext.y1 + ext.y2) / 2,
            },
            style: {
              width: ext.x2 - ext.x1,
              height: ext.y2 - ext.y1,
              shape: 'rectangle',
              'background-color': 'transparent',
              'border-width': 1,
              'border-color': COLORS.accentPrimary,
              'border-opacity': 0.6,
              'background-opacity': 0.05,
              label: '',
              events: 'no',
            } as unknown as cytoscape.Css.Node,
          });
        }
      };
      const updateMinimap = () => {
        if (minimapTimer !== undefined) return;
        minimapTimer = setTimeout(() => {
          minimapTimer = undefined;
          updateMinimapImmediate();
        }, 50);
      };

      // Listen for viewport-changing events on the main graph.
      cy.on('pan zoom', updateMinimap);
      cy.on('position', 'node', updateMinimap);
      cy.on('add remove', updateMinimap);
      cy.on('layoutstop', updateMinimap);

      // Initial draw (immediate, no debounce).
      updateMinimapImmediate();
    }

    return () => {
      cy.destroy();
      cyRef.current = null;
      if (minimapCyRef.current) {
        minimapCyRef.current.destroy();
        minimapCyRef.current = null;
      }
    };
  }, [graphData, projectColorMap, navigate]);

  // Apply client-side filters (project + tag).
  useEffect(() => {
    if (!cyRef.current) return;
    const cy = cyRef.current;
    const hasProjectFilter = selectedProjects.size > 0;
    const hasTagFilter = activeTags.size > 0;

    cy.nodes().forEach((node) => {
      let visible = true;

      if (hasProjectFilter) {
        const pid = node.data('projectId');
        if (!pid || !selectedProjects.has(pid)) {
          visible = false;
        }
      }

      if (hasTagFilter && visible) {
        const nodeTags: string[] = node.data('tags') || [];
        const matchesAnyTag = nodeTags.some((t) => activeTags.has(t));
        if (!matchesAnyTag) {
          visible = false;
        }
      }

      if (visible) {
        node.show();
      } else {
        node.hide();
      }
    });

    cy.edges().forEach((edge) => {
      const src = edge.source();
      const tgt = edge.target();
      if (src.hidden() || tgt.hidden()) {
        edge.hide();
      } else {
        edge.show();
      }
    });
  }, [selectedProjects, activeTags]);

  const handleProjectToggle = (projectId: string) => {
    setSelectedProjects((prev) => {
      const next = new Set(prev);
      if (next.has(projectId)) {
        next.delete(projectId);
      } else {
        next.add(projectId);
      }
      return next;
    });
  };

  const handleTagToggle = (tagName: string) => {
    setActiveTags((prev) => {
      const next = new Set(prev);
      if (next.has(tagName)) {
        next.delete(tagName);
      } else {
        next.add(tagName);
      }
      return next;
    });
  };

  const handleReset = () => {
    setActiveTags(new Set());
    setSelectedProjects(new Set());
    setSinceDate('');
    setUntilDate('');
  };

  // Keyboard navigation for graph nodes (I-M33).
  const handleGraphKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (!cyRef.current) return;
    const cy = cyRef.current;
    const visibleNodes = cy.nodes(':visible');
    if (visibleNodes.length === 0) return;

    if (e.key === 'Tab') {
      e.preventDefault();
      const nextIndex = e.shiftKey
        ? (focusedNodeIndex <= 0 ? visibleNodes.length - 1 : focusedNodeIndex - 1)
        : (focusedNodeIndex + 1) % visibleNodes.length;
      setFocusedNodeIndex(nextIndex);
      const node = visibleNodes[nextIndex];
      cy.elements().unselect();
      node.select();
      cy.animate({ center: { eles: node }, duration: 200 });
    } else if (e.key === 'Enter' && focusedNodeIndex >= 0 && focusedNodeIndex < visibleNodes.length) {
      const node = visibleNodes[focusedNodeIndex];
      navigate(`/notes/${node.id()}`);
    } else if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) {
      e.preventDefault();
      if (focusedNodeIndex < 0) {
        setFocusedNodeIndex(0);
        const node = visibleNodes[0];
        cy.elements().unselect();
        node.select();
        cy.animate({ center: { eles: node }, duration: 200 });
        return;
      }
      const currentNode = visibleNodes[focusedNodeIndex];
      const currentPos = currentNode.position();
      let bestNode: cytoscape.NodeSingular | null = null;
      let bestDist = Infinity;

      visibleNodes.forEach((node) => {
        if (node.id() === currentNode.id()) return;
        const pos = node.position();
        const dx = pos.x - currentPos.x;
        const dy = pos.y - currentPos.y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        let isDirectional = false;
        if (e.key === 'ArrowRight' && dx > 0) isDirectional = true;
        if (e.key === 'ArrowLeft' && dx < 0) isDirectional = true;
        if (e.key === 'ArrowDown' && dy > 0) isDirectional = true;
        if (e.key === 'ArrowUp' && dy < 0) isDirectional = true;

        if (isDirectional && dist < bestDist) {
          bestDist = dist;
          bestNode = node;
        }
      });

      if (bestNode) {
        const idx = visibleNodes.indexOf(bestNode);
        setFocusedNodeIndex(idx);
        cy.elements().unselect();
        (bestNode as cytoscape.NodeSingular).select();
        cy.animate({ center: { eles: bestNode }, duration: 200 });
      }
    } else if (e.key === 'Escape') {
      setFocusedNodeIndex(-1);
      cy.elements().unselect();
    }
  }, [focusedNodeIndex, navigate]);

  if (loading && !graphData) {
    return (
      <div className={styles.container}>
        <GraphSkeleton />
      </div>
    );
  }

  if (error && !graphData) {
    return (
      <div className={styles.container}>
        <div className={styles.errorState}>
          <div className={styles.errorTitle}>Failed to load graph</div>
          <div className={styles.errorDescription}>{error}</div>
          <button className={styles.retryButton} onClick={fetchGraph}>
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (graphData && graphData.nodes.length === 0) {
    return (
      <div className={styles.container}>
        <div className={styles.emptyState}>
          <div className={styles.emptyTitle}>No notes yet</div>
          <div className={styles.emptyDescription}>
            Create some notes with [[wikilinks]] to see your knowledge graph
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      {/* Visually hidden instructions for keyboard users */}
      <div className={styles.srOnly}>
        Use Tab to focus graph nodes, Arrow keys to navigate between nodes,
        Enter to open a note, and Escape to deselect.
      </div>
      <div
        ref={containerRef}
        className={styles.canvas}
        role="img"
        aria-label="Knowledge graph showing connections between notes"
        tabIndex={0}
        onKeyDown={handleGraphKeyDown}
      />

      {/* Node tooltip */}
      {tooltip && (
        <div
          className={styles.tooltip}
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          {tooltip.text}
        </div>
      )}

      {/* Minimap */}
      <div ref={minimapRef} className={styles.minimap} />

      <motion.div
        className={styles.filterPanel}
        initial={{ opacity: 0, x: -12 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
      >
        <div className={styles.filterTitle}>Filters</div>

        {projects.length > 0 && (
          <div className={styles.filterSection}>
            <div className={styles.filterSectionLabel}>Projects</div>
            {projects.map((project, index) => (
              <label key={project.id} className={styles.checkboxRow}>
                <input
                  type="checkbox"
                  checked={selectedProjects.has(project.id)}
                  onChange={() => handleProjectToggle(project.id)}
                />
                <span
                  className={styles.projectDot}
                  style={{ backgroundColor: getProjectColor(index) }}
                />
                {project.name}
              </label>
            ))}
          </div>
        )}

        {tags.length > 0 && (
          <div className={styles.filterSection}>
            <div className={styles.filterSectionLabel}>Tags</div>
            <div className={styles.tagPills}>
              {tags.slice(0, 15).map((tag) => (
                <button
                  key={tag.name}
                  className={`${styles.tagPill} ${activeTags.has(tag.name) ? styles.tagPillActive : ''}`}
                  onClick={() => handleTagToggle(tag.name)}
                >
                  #{tag.name}
                </button>
              ))}
            </div>
          </div>
        )}

        <div className={styles.filterSection}>
          <div className={styles.filterSectionLabel}>Date range</div>
          <div className={styles.dateInputs}>
            <input
              type="date"
              className={styles.dateInput}
              value={sinceDate}
              onChange={(e) => setSinceDate(e.target.value)}
              aria-label="Since date"
              placeholder="Since"
            />
            <input
              type="date"
              className={styles.dateInput}
              value={untilDate}
              onChange={(e) => setUntilDate(e.target.value)}
              aria-label="Until date"
              placeholder="Until"
            />
          </div>
        </div>

        <button className={styles.resetButton} onClick={handleReset}>
          Reset filters
        </button>
      </motion.div>
    </div>
  );
}
