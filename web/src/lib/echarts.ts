import { BarChart, LineChart } from "echarts/charts";
import {
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from "echarts/components";
// oxlint-disable-next-line import/no-namespace, unicorn/prefer-export-from -- Kumo's charts take the whole ECharts core, registered below before it leaves
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

// Kumo's charts take the ECharts core so the app decides which modules ship.
// Register here once; every chart imports this module instead of echarts/core.
// The brush and its toolbox are what let a time series be dragged across to zoom into a range.
echarts.use([
  BarChart,
  LineChart,
  GridComponent,
  TooltipComponent,
  BrushComponent,
  ToolboxComponent,
  CanvasRenderer,
]);

export { echarts };
