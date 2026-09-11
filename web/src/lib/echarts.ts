import { BarChart } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
// oxlint-disable-next-line import/no-namespace, unicorn/prefer-export-from -- Kumo's charts take the whole ECharts core, registered below before it leaves
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

// Kumo's charts take the ECharts core so the app decides which modules ship.
// Register here once; every chart imports this module instead of echarts/core.
echarts.use([BarChart, GridComponent, TooltipComponent, CanvasRenderer]);

export { echarts };
