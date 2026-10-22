// vis-network options for the Connections graph. vis-network draws on a canvas and needs concrete
// colours, so the tokens are read from the document when the graph renders (as in theme/naive.ts).
// Nodes take their entity type's chart colour (docs/ui-standard.md → Colour); other types are neutral.
import { token } from '../../theme/naive'

const typeToken: Record<string, string> = {
  PER: 'chart-4',
  ORG: 'chart-3',
  LOC: 'chart-5',
}

export function entityTypeColor(type: string) {
  const fill = token(typeToken[type] ?? 'muted-foreground')
  return { background: fill, border: fill, highlight: { background: fill, border: token('foreground') }, hover: { background: fill, border: token('foreground') } }
}

export function buildGraphOptions() {
  return {
    physics: { stabilization: { iterations: 100 } },
    edges: {
      scaling: { min: 1, max: 8 },
      smooth: { enabled: true, type: 'dynamic', roundness: 0.5 },
      color: { color: token('border'), highlight: token('muted-foreground'), hover: token('muted-foreground') },
    },
    nodes: {
      shape: 'dot',
      borderWidth: 1,
      borderWidthSelected: 2,
      font: { size: 12, color: token('foreground'), face: token('font-sans') },
    },
    interaction: { hover: true, tooltipDelay: 200 },
  }
}
