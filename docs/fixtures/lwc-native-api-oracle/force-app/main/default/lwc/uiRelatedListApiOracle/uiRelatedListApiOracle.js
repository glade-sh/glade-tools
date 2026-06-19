import { LightningElement } from "lwc";
import * as api from "lightning/uiRelatedListApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiRelatedListApi";
  exports = Object.keys(api || {}).join(",");
}
